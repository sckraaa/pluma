package fixture

import (
	"context"

	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// A channel's committed state read inside a box transaction is nested;
// opening the channel in it is the way.
func (d *Dao) channels(ctx context.Context) error {
	return d.RunBox(ctx, 1, func(b *userbox.Box) error {
		if _, err := channelbox.Read(ctx, d.db, 2); err != nil {
			return err
		}
		if _, err := channelbox.Open(b, 2); err != nil {
			return err
		}
		_, err := channelbox.CheckJournal(ctx, d.db, 2, 1)
		return err
	})
}
