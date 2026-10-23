package userbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Check verifies, at the start of a service, the MySQL settings Run relies
// on, and names each one that is wrong:
//
//   - the session innodb_lock_wait_timeout of the pool is at most
//     LockWaitTimeout (DSN parameter innodb_lock_wait_timeout=2);
//   - innodb_flush_log_at_trx_commit = 1;
//   - with the binary log on: binlog_format = ROW (READ COMMITTED needs
//     row-based logging) and sync_binlog = 1.
//
// A client sees a pts as soon as its transaction commits, so a commit lost
// by a MySQL crash would roll pts back under the client, as a Redis loss
// used to.
func Check(ctx context.Context, db *sql.DB) error {
	var (
		lockWait, flushLog, logBin, syncBinlog int64
		binlogFormat                           string
	)
	err := db.QueryRowContext(ctx, "SELECT @@SESSION.innodb_lock_wait_timeout, @@GLOBAL.innodb_flush_log_at_trx_commit, @@GLOBAL.log_bin, @@GLOBAL.binlog_format, @@GLOBAL.sync_binlog").
		Scan(&lockWait, &flushLog, &logBin, &binlogFormat, &syncBinlog)
	if err != nil {
		return fmt.Errorf("userbox: read MySQL settings: %w", err)
	}

	maxLockWait := int64(LockWaitTimeout.Seconds())
	var errs []error
	if lockWait > maxLockWait {
		errs = append(errs, fmt.Errorf("innodb_lock_wait_timeout is %d s, want at most %d s: add innodb_lock_wait_timeout=%d to the MySQL DSN", lockWait, maxLockWait, maxLockWait))
	}
	if flushLog != 1 {
		errs = append(errs, fmt.Errorf("innodb_flush_log_at_trx_commit is %d, want 1", flushLog))
	}
	if logBin != 0 {
		if !strings.EqualFold(binlogFormat, "ROW") {
			errs = append(errs, fmt.Errorf("binlog_format is %s, want ROW", binlogFormat))
		}
		if syncBinlog != 1 {
			errs = append(errs, fmt.Errorf("sync_binlog is %d, want 1", syncBinlog))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("userbox: MySQL settings: %w", errors.Join(errs...))
	}
	return nil
}

// CheckRPCTimeout verifies the deadline of an RPC, in milliseconds as in a
// go-zero config (name is its key), whose handler runs a box operation that
// takes up to need: 0 (no deadline) or at least need. A shorter deadline
// ends a request waiting for a busy user's lock with a bare
// DeadlineExceeded instead of the operation's own error, and a COMMIT that
// went through then answers the client with an error.
func CheckRPCTimeout(name string, timeoutMs int64, need time.Duration) error {
	if timeoutMs == 0 || time.Duration(timeoutMs)*time.Millisecond >= need {
		return nil
	}
	return fmt.Errorf("userbox: %s is %d ms, want 0 (no deadline) or at least %d ms: the box operations behind it take up to %v",
		name, timeoutMs, need.Milliseconds(), need)
}
