// Command pluma-admin manages who may log in to the server and issues login
// codes when they cannot be delivered in the app (task 001, closed
// registration). It talks to MySQL and Redis with the bff config.
//
// Usage:
//
//	pluma-admin [-f bff.yaml] allowlist add <phone> [note...]
//	pluma-admin [-f bff.yaml] allowlist remove <phone>
//	pluma-admin [-f bff.yaml] allowlist list
//	pluma-admin [-f bff.yaml] login-code <phone> [request-id]
//	pluma-admin [-f bff.yaml] login-unblock <phone|ip>
//	pluma-admin [-f bff.yaml] state-raise -by <margin> [-user <id>]
//	pluma-admin [-f bff.yaml] state-export-legacy -margin <margin>
//	pluma-admin [-f bff.yaml] verify-pts [-after <pts>] <user id>
//	pluma-admin [-f bff.yaml] outbox list [-state <state>] [-user <id>] [-limit <n>]
//	pluma-admin [-f bff.yaml] outbox replay -user <id>
//	pluma-admin [-f bff.yaml] outbox discard -id <event id>
//
// login-code generates a fresh code for a pending code request of the phone
// (auth.sendCode or account.sendChangePhoneCode that answered "code sent by
// SMS"), stores only its HMAC in place of the previous code and prints it.
// Anyone who knows the phone can create a request, so when there are several
// it lists them (time, address, checks used) and the administrator picks one
// by its id. Give the code to the user over a channel you trust.
//
// login-unblock clears the login flood counters of a phone (code requests and
// wrong codes; FLOOD_WAIT on every login attempt) or of a client address.
//
// state-raise, state-export-legacy and verify-pts maintain the state of
// users' boxes (user_state, ADR 0011, pp. 10-11; docs/ops/user-state.md).
// The first two run only while msg and biz are stopped: state-raise lifts
// every user's (or one user's) pts and box ids above everything written,
// after a rollback or a restore from a backup; state-export-legacy writes
// the counters to Redis for a rollback to a version before task 002.
// verify-pts checks that a user's journal has no gaps or overlaps.
//
// outbox shows and repairs the delivery of events (mq_outbox, ADR 0012,
// p. 5): list prints the rows without their content (state pending,
// published, dead or parked); replay returns a user's dead and parked
// events to pending, to be applied in order (after the cause is fixed);
// discard deletes one event for good. replay and discard print who did
// what on stderr.
//
// From the repo root (services run with teamgramd/etc, secrets in .env):
//
//	set -a; . ./.env; set +a; go run ./tools/pluma-admin allowlist add +79991234567 Alice
//
// In Docker: docker compose exec teamgram /app/bin/pluma-admin -f /app/etc2/bff.yaml login-code +79991234567
//
// See docs/ops/security-setup.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/logincode"
	"github.com/teamgram/teamgram-server/pkg/phoneallowlist"
	"github.com/teamgram/teamgram-server/pkg/phonenumber"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// config is the part of the bff config the tool needs.
type config struct {
	Mysql     sqlx.Config
	KV        kv.KvConf
	LoginCode logincode.Config
}

// allowlist is what the tool changes in the phone allowlist.
type allowlist interface {
	logincode.Allowlist
	Add(ctx context.Context, phone, note string) (bool, error)
	Remove(ctx context.Context, phone string) (bool, error)
	List(ctx context.Context) ([]phoneallowlist.Entry, error)
}

// codeIssuer issues login codes for pending requests and clears flood limits.
type codeIssuer interface {
	Pending(ctx context.Context, phone string) ([]logincode.PendingRequest, error)
	Reissue(ctx context.Context, phone, requestId string) (*logincode.Reissued, error)
	Unblock(ctx context.Context, phone string) error
	UnblockIP(ctx context.Context, ip string) error
}

// backend connects to the stores named by the config file.
type backend func(configFile string) (allowlist, codeIssuer, error)

func main() {
	logx.Disable()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, connect))
}

func connect(configFile string) (allowlist, codeIssuer, error) {
	var c config
	if err := conf.Load(configFile, &c); err != nil {
		return nil, nil, fmt.Errorf("config %s: %w", configFile, err)
	}
	list := phoneallowlist.New(sqlx.NewMySQL(&c.Mysql))
	codes, err := logincode.NewService(c.LoginCode, list, kv.NewStore(c.KV))
	if err != nil {
		return nil, nil, err
	}
	return list, codes, nil
}

const usage = `Usage:
  pluma-admin [-f bff.yaml] allowlist add <phone> [note...]
  pluma-admin [-f bff.yaml] allowlist remove <phone>
  pluma-admin [-f bff.yaml] allowlist list
  pluma-admin [-f bff.yaml] login-code <phone> [request-id]
  pluma-admin [-f bff.yaml] login-unblock <phone|ip>
  pluma-admin [-f bff.yaml] state-raise -by <margin> [-user <id>]
  pluma-admin [-f bff.yaml] state-export-legacy -margin <margin>
  pluma-admin [-f bff.yaml] verify-pts [-after <pts>] <user id>
  pluma-admin [-f bff.yaml] outbox list [-state pending|published|dead|parked] [-user <id>] [-limit <n>]
  pluma-admin [-f bff.yaml] outbox replay -user <id>
  pluma-admin [-f bff.yaml] outbox discard -id <event id>
  pluma-admin [-f bff.yaml] migrate up [-dir <sql dir>] [-wait <duration>]
  pluma-admin [-f bff.yaml] migrate status [-dir <sql dir>]
  pluma-admin [-f bff.yaml] migrate baseline -through <file> [-dir <sql dir>]
  pluma-admin -f dfs.yaml storage init [-admin <url>] [-wait <duration>]   (GARAGE_ADMIN_TOKEN)

Phones are international numbers, e.g. +79991234567.
state-raise and state-export-legacy run only while msg and biz are stopped.
`

var errUsage = errors.New("usage")

// run executes the command in args and returns the exit code: 0 success,
// 1 failure, 2 bad usage.
func run(args []string, stdout, stderr io.Writer, connect backend) int {
	flags := flag.NewFlagSet("pluma-admin", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {
		fmt.Fprint(stderr, usage)
		fmt.Fprintln(stderr, "\nFlags:")
		flags.PrintDefaults()
	}
	configFile := flags.String("f", "teamgramd/etc/bff.yaml", "bff config file (secrets come from the environment, see .env)")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	err := dispatch(flags.Args(), *configFile, stdout, connect)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		flags.Usage()
		return 2
	default:
		fmt.Fprintln(stderr, "pluma-admin:", err)
		return 1
	}
}

func dispatch(args []string, configFile string, stdout io.Writer, connect backend) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "state-raise", "state-export-legacy", "verify-pts":
		return boxCommand(args, configFile, stdout)
	case "outbox":
		return outboxCommand(args, configFile, stdout)
	case "migrate":
		return migrateCommand(args, configFile, stdout)
	case "storage":
		return storageCommand(args, configFile, stdout)
	}
	var cmd func(ctx context.Context, list allowlist, codes codeIssuer) error
	switch {
	case args[0] == "allowlist" && len(args) >= 3 && args[1] == "add":
		phone, err := normalize(args[2])
		if err != nil {
			return err
		}
		note := strings.Join(args[3:], " ")
		cmd = func(ctx context.Context, list allowlist, _ codeIssuer) error {
			added, err := list.Add(ctx, phone, note)
			if err != nil {
				return err
			}
			if added {
				fmt.Fprintf(stdout, "+%s added to the allowlist.\n", phone)
			} else {
				fmt.Fprintf(stdout, "+%s is already on the allowlist.\n", phone)
			}
			return nil
		}
	case args[0] == "allowlist" && len(args) == 3 && args[1] == "remove":
		phone, err := normalize(args[2])
		if err != nil {
			return err
		}
		cmd = func(ctx context.Context, list allowlist, _ codeIssuer) error {
			removed, err := list.Remove(ctx, phone)
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("+%s is not on the allowlist", phone)
			}
			fmt.Fprintf(stdout, "+%s removed from the allowlist. Its existing sessions stay active: end them in the app (Settings > Devices) if needed.\n", phone)
			return nil
		}
	case args[0] == "allowlist" && len(args) == 2 && args[1] == "list":
		cmd = func(ctx context.Context, list allowlist, _ codeIssuer) error {
			entries, err := list.List(ctx)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PHONE\tADDED\tNOTE")
			for _, e := range entries {
				fmt.Fprintf(w, "+%s\t%s\t%s\n", e.Phone, time.Unix(e.CreatedAt, 0).Format("2006-01-02 15:04"), e.Note)
			}
			if err = w.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%d phone(s).\n", len(entries))
			return nil
		}
	case args[0] == "login-code" && (len(args) == 2 || len(args) == 3):
		phone, err := normalize(args[1])
		if err != nil {
			return err
		}
		var requestId string
		if len(args) == 3 {
			requestId = args[2]
		}
		cmd = func(ctx context.Context, _ allowlist, codes codeIssuer) error {
			return loginCode(ctx, stdout, codes, phone, requestId)
		}
	case args[0] == "login-unblock" && len(args) == 2:
		if ip, err := netip.ParseAddr(strings.TrimSpace(args[1])); err == nil {
			cmd = func(ctx context.Context, _ allowlist, codes codeIssuer) error {
				if err := codes.UnblockIP(ctx, ip.String()); err != nil {
					return err
				}
				fmt.Fprintf(stdout, "Login limits of %s cleared (an IPv6 address clears its /64).\n", ip)
				return nil
			}
			break
		}
		phone, err := normalize(args[1])
		if err != nil {
			return err
		}
		cmd = func(ctx context.Context, _ allowlist, codes codeIssuer) error {
			if err := codes.Unblock(ctx, phone); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Login limits of +%s cleared: code requests and wrong codes count from zero.\n", phone)
			return nil
		}
	default:
		return errUsage
	}

	list, codes, err := connect(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return cmd(ctx, list, codes)
}

// loginCode issues a code for the chosen pending request of phone, or lists
// the requests when the choice is not clear.
func loginCode(ctx context.Context, stdout io.Writer, codes codeIssuer, phone, requestId string) error {
	r, err := codes.Reissue(ctx, phone, requestId)
	if errors.Is(err, logincode.ErrAmbiguousRequest) || (requestId != "" && errors.Is(err, logincode.ErrNoPendingRequest)) {
		list, listErr := codes.Pending(ctx, phone)
		if listErr != nil {
			return listErr
		}
		if len(list) > 0 {
			fmt.Fprintf(stdout, "Pending login code requests for +%s:\n", phone)
			printRequests(stdout, list)
			fmt.Fprintf(stdout, "Anyone who knows the phone can make a request: ask the owner when and from where they asked, then run: login-code +%s <ID>\n", phone)
		}
		return err
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Login code for +%s: %s\n", phone, r.Code)
	fmt.Fprintf(stdout, "Valid until %s. Request %s made at %s from %s; checks used: %d of %d.\n",
		r.ExpiresAt.Format("15:04:05 MST"),
		r.Id,
		r.RequestedAt.Format("2006-01-02 15:04:05 MST"),
		orUnknown(r.ClientIp),
		r.Attempts, logincode.MaxAttempts)
	fmt.Fprintln(stdout, "Give it only to the owner of the phone, over a channel you trust. It is not stored and cannot be shown again.")
	return nil
}

func printRequests(stdout io.Writer, list []logincode.PendingRequest) {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tREQUESTED\tFROM\tCHECKS USED\tCODE ISSUED\tENDS")
	for _, r := range list {
		issued := "no"
		if r.HasCode {
			issued = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d of %d\t%s\t%s\n", r.Id,
			r.RequestedAt.Format("2006-01-02 15:04:05 MST"), orUnknown(r.ClientIp),
			r.Attempts, logincode.MaxAttempts, issued, r.EndsAt.Format("15:04:05 MST"))
	}
	_ = w.Flush()
}

// normalize turns a phone as typed by the administrator into the stored form
// (digits without '+'), the same way the server normalizes client input.
func normalize(phone string) (string, error) {
	_, digits, err := phonenumber.CheckPhoneNumberInvalid(strings.TrimSpace(phone))
	if err != nil {
		return "", fmt.Errorf("invalid phone number %q: use the international format, e.g. +79991234567", phone)
	}
	return digits, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "an unknown address"
	}
	return s
}
