package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// The events of mq_outbox (ADR 0012, p. 5): what waits, what is dead or
// parked, and the operator's way out. The topics are the ones topics.sh
// creates; the push topic of bff comes from its config.
const (
	inboxTopic   = "Inbox-T"
	botSyncTopic = "BotSync"
)

// outboxConfig is the part of the bff config the outbox commands need.
type outboxConfig struct {
	Mysql      sqlx.Config
	SyncClient struct {
		Topic string
	}
}

// openOutbox connects to the database of mq_outbox with the session
// settings of the box transactions (replay and discard of inbox events run
// in them) and registers the topics.
var openOutbox = func(configFile string) (*boxStores, error) {
	var c outboxConfig
	if err := conf.Load(configFile, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", configFile, err)
	}
	if err := outbox.Register(outbox.Topics{Inbox: []string{inboxTopic}, Push: []string{c.SyncClient.Topic, botSyncTopic}}); err != nil {
		return nil, err
	}
	dsn, err := mysql.ParseDSN(c.Mysql.DSN)
	if err != nil {
		return nil, err
	}
	if dsn.Params == nil {
		dsn.Params = map[string]string{}
	}
	dsn.Params["innodb_lock_wait_timeout"] = strconv.Itoa(int(userbox.LockWaitTimeout.Seconds()))
	db, err := userbox.Open(dsn.FormatDSN())
	if err != nil {
		return nil, err
	}
	return &boxStores{db: db}, nil
}

// operator names who runs the command, for the record of what was done.
func operator() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return "unknown"
}

// audit records a change made by the operator on stderr: who, what, which
// event; never its content.
func audit(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "pluma-admin audit %s %s: %s\n", time.Now().UTC().Format(time.RFC3339), operator(), fmt.Sprintf(format, args...))
}

// outboxCommand runs outbox list, replay or discard.
func outboxCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) < 2 {
		return errUsage
	}
	flags := flag.NewFlagSet("outbox "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var (
		state = flags.String("state", "", "")
		uid   = flags.Int64("user", 0, "")
		id    = flags.Int64("id", 0, "")
		limit = flags.Int("limit", 100, "")
	)
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
		return errUsage
	}
	var cmd func(ctx context.Context, st *boxStores) error
	switch args[1] {
	case "list":
		filter := outbox.ListFilter{Recipient: *uid, Limit: *limit}
		if *state != "" {
			s, err := userbox.ParseEventState(*state)
			if err != nil {
				return err
			}
			filter.State = &s
		}
		cmd = func(ctx context.Context, st *boxStores) error {
			rows, err := outbox.List(ctx, st.db, filter)
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATE\tTOPIC\tTO\tFROM\tMETHOD\tCREATED\tPUBLISHED\tREDELIVERED\tDEFERRED\tATTEMPTS\tREPLAYS\tDUE\tERROR")
			for _, r := range rows {
				due := ""
				if !r.DueAt.IsZero() {
					due = r.DueAt.Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%d\t%d\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n",
					r.Id, r.State, r.Topic, r.Recipient, r.Initiator, r.Method, r.CreatedAt.Format("2006-01-02 15:04:05"),
					r.PublishCount, r.Redeliveries, r.Deferrals, r.Attempts, r.Replays, due, r.Error)
			}
			if err = w.Flush(); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "%d row(s).\n", len(rows))
			return nil
		}
	case "replay":
		if *uid <= 0 {
			return errUsage
		}
		cmd = func(ctx context.Context, st *boxStores) error {
			n, err := outbox.Replay(ctx, st.db, *uid)
			if err != nil {
				return err
			}
			audit("replayed %d dead and parked event(s) of user %d", n, *uid)
			fmt.Fprintf(stdout, "%d dead and parked event(s) of user %d returned to pending, in id order.\n", n, *uid)
			return nil
		}
	case "discard":
		if *id <= 0 {
			return errUsage
		}
		cmd = func(ctx context.Context, st *boxStores) error {
			info, err := outbox.Discard(ctx, st.db, *id)
			if errors.Is(err, outbox.ErrNoRow) {
				return fmt.Errorf("no event %d", *id)
			}
			if err != nil {
				return err
			}
			audit("discarded event %d (%s, %s to user %d, %s)", info.Id, info.State, info.Topic, info.Recipient, info.Method)
			fmt.Fprintf(stdout, "Event %d (%s, %s to user %d, %s) discarded: it will never be delivered.\n", info.Id, info.State, info.Topic, info.Recipient, info.Method)
			if class, _ := userbox.TopicClassOf(info.Topic); class == userbox.TopicInbox {
				fmt.Fprintf(stdout, "The later events of user %d stay parked until `outbox replay -user %d`.\n", info.Recipient, info.Recipient)
			}
			return nil
		}
	default:
		return errUsage
	}

	st, err := openOutbox(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return cmd(ctx, st)
}
