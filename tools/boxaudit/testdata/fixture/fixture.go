// Package fixture breaks each rule of boxaudit once or twice; the test
// checks the findings by line.
package fixture

import (
	"context"
	"database/sql"
	"errors"

	msqlx "github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

type MessagesDAO struct{}

func (*MessagesDAO) Select(context.Context) error { return nil }
func (*MessagesDAO) SelectTx(*msqlx.Tx) error     { return nil }
func (*MessagesDAO) Delete(context.Context) (int64, error) {
	return 0, nil
}

type UserClient interface {
	UserGetUser(ctx context.Context, id int64) (string, error)
}

type CachedConn struct{}

func (CachedConn) DelCache(context.Context, ...string) error { return nil }

type IdGenerator interface {
	NextId(ctx context.Context) (int64, error)
}

type Dao struct {
	*MessagesDAO
	UserClient
	CachedConn
	IdGenerator
	db *sql.DB
}

func (d *Dao) RunBox(ctx context.Context, userId int64, fn func(b *userbox.Box) error) error {
	_, err := userbox.Run(ctx, d.db, userId, fn)
	return err
}

func mayFail() error { return errors.New("failed") }

func ok(b *userbox.Box) error {
	if err := mayFail(); err != nil {
		return err
	}
	b.InvalidateCache("k")
	return nil
}

func (d *Dao) violations(ctx context.Context) error {
	return d.RunBox(ctx, 1, func(b *userbox.Box) error {
		mayFail()
		_ = mayFail()
		_, _ = d.Delete(ctx)
		_ = d.Select(ctx)
		_, _ = d.UserGetUser(ctx, 1)
		_ = d.DelCache(ctx, "k")
		_, _ = d.NextId(ctx)
		_, _ = userbox.Read(ctx, d.db, 1)
		_ = d.RunBox(ctx, 2, ok)
		if err := d.SelectTx(b.Tx()); err != nil {
			return err
		}
		// boxaudit:ok the fixture shows a marked line
		_ = mayFail()
		return ok(b)
	})
}

// outside is not box code: nothing is reported.
func (d *Dao) outside(ctx context.Context) {
	_ = d.Select(ctx)
	_, _ = d.UserGetUser(ctx, 1)
}

// helper takes a Box: box code.
func (d *Dao) helper(ctx context.Context, b *userbox.Box) {
	_ = d.Select(ctx)
	mayFail()
	_ = b
}
