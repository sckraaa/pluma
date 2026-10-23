package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// boxStores are the stores of the box state commands: MySQL and the Redis
// the counters lived in before task 002 (the KV section of bff.yaml; in the
// shipped configs the same server as idgen's former SeqIDGen).
type boxStores struct {
	db *sql.DB
	kv kv.Store
}

// connectBoxes connects the box state commands.
var connectBoxes = func(configFile string) (*boxStores, error) {
	var c config
	if err := conf.Load(configFile, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", configFile, err)
	}
	db, err := userbox.Open(c.Mysql.DSN)
	if err != nil {
		return nil, err
	}
	return &boxStores{db: db, kv: kv.NewStore(c.KV)}, nil
}

// boxCommand runs state-raise, state-export-legacy or verify-pts.
func boxCommand(args []string, configFile string, stdout io.Writer) error {
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var (
		by     = flags.Int("by", 0, "")
		user   = flags.Int64("user", 0, "")
		margin = flags.Int("margin", 0, "")
		after  = flags.Int("after", userbox.BasePts, "")
	)
	if err := flags.Parse(args[1:]); err != nil {
		return errUsage
	}
	var cmd func(ctx context.Context, st *boxStores) error
	switch args[0] {
	case "state-raise":
		if *by <= 0 || flags.NArg() != 0 {
			return errUsage
		}
		cmd = func(ctx context.Context, st *boxStores) error {
			raised, added, err := userbox.Raise(ctx, st.db, int32(*by), *user)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "user_state: %d row(s) raised by %d above the journal and the messages, %d added.\n", raised, *by, added)
			return nil
		}
	case "state-export-legacy":
		if *margin <= 0 || flags.NArg() != 0 {
			return errUsage
		}
		cmd = func(ctx context.Context, st *boxStores) error {
			n, err := userbox.ExportLegacy(ctx, st.db, int32(*margin), st.kv.SetCtx)
			if err != nil {
				return err
			}
			fmt.Fprintf(stdout, "Counters of %d user(s) written to Redis (%s*, %s*) with margin %d.\n",
				n, userbox.LegacyPtsKeyPrefix, userbox.LegacyMessageBoxKeyPrefix, *margin)
			return nil
		}
	case "verify-pts":
		if flags.NArg() != 1 {
			return errUsage
		}
		userId, err := strconv.ParseInt(flags.Arg(0), 10, 64)
		if err != nil || userId <= 0 {
			return fmt.Errorf("invalid user id %q", flags.Arg(0))
		}
		cmd = func(ctx context.Context, st *boxStores) error {
			return verifyPts(ctx, stdout, st.db, userId, int32(*after))
		}
	}

	st, err := connectBoxes(configFile)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return cmd(ctx, st)
}

var errJournal = errors.New("the journal does not cover the state once")

// verifyPts reports the gaps and overlaps of userId's journal after the pts
// after up to the committed state.
func verifyPts(ctx context.Context, stdout io.Writer, db *sql.DB, userId int64, after int32) error {
	rep, err := userbox.CheckJournal(ctx, db, userId, after)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "User %d: state pts %d, %d journal row(s) in (%d, %d].\n", userId, rep.State.Pts, rep.Rows, after, rep.State.Pts)
	for _, g := range rep.Gaps {
		fmt.Fprintf(stdout, "  gap: pts (%d, %d] are in no row\n", g.From, g.To)
	}
	for _, o := range rep.Overlaps {
		fmt.Fprintf(stdout, "  overlap: pts (%d, %d] are in more than one row\n", o.From, o.To)
	}
	if !rep.OK() {
		fmt.Fprintln(stdout, "A gap of the margin of migrate-20261022.sql or of state-raise at the time it ran is that jump, not a loss.")
		return errJournal
	}
	fmt.Fprintln(stdout, "OK: every pts is in exactly one row.")
	return nil
}
