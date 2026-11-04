package reactions

import (
	"context"
	"database/sql"
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// Querier reads rows outside a box transaction: the *sqlx.DB of a service.
type Querier interface {
	QueryRowsPartial(ctx context.Context, v interface{}, query string, args ...interface{}) error
}

// reactionRow is a row of message_reactions (task 053) as go-zero scans it.
type reactionRow struct {
	DialogMessageId int64  `db:"dialog_message_id"`
	UserId          int64  `db:"user_id"`
	Reaction        string `db:"reaction"`
	ChosenOrder     int32  `db:"chosen_order"`
	Big             bool   `db:"big"`
	Unread          bool   `db:"unread"`
	Date            int64  `db:"date"`
}

// Load returns the reactions to the messages dialogMessageIds by
// message, in the order of their dates. Read from the master: a client
// reads the reactions right after it set them.
func Load(ctx context.Context, q Querier, dialogMessageIds []int64) (map[int64][]Row, error) {
	out := map[int64][]Row{}
	if len(dialogMessageIds) == 0 {
		return out, nil
	}
	args := make([]any, 0, len(dialogMessageIds))
	for _, id := range dialogMessageIds {
		args = append(args, id)
	}
	var rows []reactionRow
	if err := q.QueryRowsPartial(ctx, &rows,
		"SELECT dialog_message_id, user_id, reaction, chosen_order, big, unread, `date` FROM message_reactions WHERE dialog_message_id IN (?"+strings.Repeat(", ?", len(args)-1)+") ORDER BY `date`, user_id, chosen_order",
		args...); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.DialogMessageId] = append(out[r.DialogMessageId], Row{
			UserId: r.UserId, Reaction: r.Reaction, ChosenOrder: r.ChosenOrder, Big: r.Big, Unread: r.Unread, Date: r.Date,
		})
	}
	return out, nil
}

// BoxKind is the kind of chat of a copy of a message, 0 for one reactions of
// this package do not cover (channels).
func BoxKind(box *mtproto.MessageBox) Kind {
	switch box.GetPeerType() {
	case mtproto.PEER_CHAT:
		return Group
	case mtproto.PEER_USER:
		if box.GetPeerId() == box.GetUserId() {
			return Saved
		}
		return Private
	}
	return 0
}

// Attach sets the reactions of each message of boxes as the owner
// of its copy sees them, with one read for all: every message the clients
// get carries its reactions (Desktop clears them from a message without the
// field, history_item.cpp:2240).
func Attach(ctx context.Context, q Querier, boxes ...*mtproto.MessageBox) error {
	ids := make([]int64, 0, len(boxes))
	for _, box := range boxes {
		if box.GetMessage() != nil && box.GetDialogMessageId() != 0 && BoxKind(box) != 0 {
			ids = append(ids, box.GetDialogMessageId())
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := Load(ctx, q, ids)
	if err != nil {
		return err
	}
	for _, box := range boxes {
		kind := BoxKind(box)
		if box.GetMessage() == nil || kind == 0 {
			continue
		}
		box.Message.Reactions = View(rows[box.GetDialogMessageId()], box.GetUserId(), box.GetSenderUserId(), kind)
	}
	return nil
}

// Execer writes outside a box transaction: the *sqlx.DB of a service.
type Execer interface {
	Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

type settingsRow struct {
	DefaultReaction string `db:"default_reaction"`
	RecentClearedAt int64  `db:"recent_cleared_at"`
}

func settings(ctx context.Context, q Querier, userId int64) (settingsRow, error) {
	var rows []settingsRow
	err := q.QueryRowsPartial(ctx, &rows, "SELECT default_reaction, recent_cleared_at FROM user_reaction_settings WHERE user_id = ?", userId)
	if err != nil || len(rows) == 0 {
		return settingsRow{}, err
	}
	return rows[0], nil
}

// Default is the key of userId's default reaction, "" when it chose none.
func Default(ctx context.Context, q Querier, userId int64) (string, error) {
	s, err := settings(ctx, q, userId)
	return s.DefaultReaction, err
}

// SetDefault makes key userId's default reaction.
func SetDefault(ctx context.Context, e Execer, userId int64, key string) error {
	_, err := e.Exec(ctx, "INSERT INTO user_reaction_settings (user_id, default_reaction) VALUES (?, ?) ON DUPLICATE KEY UPDATE default_reaction = VALUES(default_reaction)", userId, key)
	return err
}

// ClearRecent forgets userId's recent reactions up to now.
func ClearRecent(ctx context.Context, e Execer, userId, now int64) error {
	_, err := e.Exec(ctx, "INSERT INTO user_reaction_settings (user_id, recent_cleared_at) VALUES (?, ?) ON DUPLICATE KEY UPDATE recent_cleared_at = VALUES(recent_cleared_at)", userId, now)
	return err
}

// Recent are the keys of the reactions userId put lately outside Saved
// Messages (where they are tags), the latest first, each once, at most
// limit, since it last cleared them.
func Recent(ctx context.Context, q Querier, userId int64, limit int32) ([]string, error) {
	s, err := settings(ctx, q, userId)
	if err != nil {
		return nil, err
	}
	var keys []string
	err = q.QueryRowsPartial(ctx, &keys,
		"SELECT r.reaction FROM message_reactions r JOIN messages m ON m.user_id = r.user_id AND m.dialog_message_id = r.dialog_message_id"+
			" WHERE r.user_id = ? AND r.`date` > ? AND NOT (m.peer_type = ? AND m.peer_id = r.user_id)"+
			" GROUP BY r.reaction ORDER BY MAX(r.`date`) DESC LIMIT ?",
		userId, s.RecentClearedAt, mtproto.PEER_USER, limit)
	return keys, err
}

// Tag is a tag of Saved Messages: a reaction, its title, the messages with it.
type Tag struct {
	Reaction string
	Title    string
	Count    int32
}

type tagRow struct {
	Reaction string `db:"reaction"`
	Count    int32  `db:"n"`
}

type titleRow struct {
	Reaction string `db:"reaction"`
	Title    string `db:"title"`
}

// Tags are the tags of userId's Saved Messages with their counts, the most
// used first, then the newest; savedPeer limits them to one saved dialog
// (nil for all). Titles of tags no message has any more are left out.
func Tags(ctx context.Context, q Querier, userId int64, savedPeer *mtproto.PeerUtil) ([]Tag, error) {
	query := "SELECT r.reaction, COUNT(*) AS n FROM message_reactions r JOIN messages m ON m.user_id = r.user_id AND m.dialog_message_id = r.dialog_message_id" +
		" WHERE r.user_id = ? AND m.peer_type = ? AND m.peer_id = ? AND m.deleted = 0"
	args := []any{userId, mtproto.PEER_USER, userId}
	if savedPeer != nil {
		query += " AND m.saved_peer_type = ? AND m.saved_peer_id = ?"
		args = append(args, savedPeer.PeerType, savedPeer.PeerId)
	}
	query += " GROUP BY r.reaction ORDER BY n DESC, MAX(r.`date`) DESC"
	var counts []tagRow
	if err := q.QueryRowsPartial(ctx, &counts, query, args...); err != nil {
		return nil, err
	}
	var titles []titleRow
	if err := q.QueryRowsPartial(ctx, &titles, "SELECT reaction, title FROM saved_reaction_tags WHERE user_id = ?", userId); err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(counts))
	for _, c := range counts {
		t := Tag{Reaction: c.Reaction, Count: c.Count}
		for _, ti := range titles {
			if ti.Reaction == c.Reaction {
				t.Title = ti.Title
			}
		}
		out = append(out, t)
	}
	return out, nil
}

// Tagged are the ids of userId's copies of its Saved Messages with all the
// tags keys, of one saved dialog when savedPeer is given, whose text has q
// when given, newest first below offsetId, at most limit.
func Tagged(ctx context.Context, q Querier, userId int64, keys []string, savedPeer *mtproto.PeerUtil, text string, offsetId, limit int32) ([]int32, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	query := "SELECT m.user_message_box_id FROM messages m JOIN message_reactions r ON r.user_id = m.user_id AND r.dialog_message_id = m.dialog_message_id" +
		" WHERE m.user_id = ? AND m.peer_type = ? AND m.peer_id = ? AND m.deleted = 0 AND m.user_message_box_id < ?" +
		" AND r.reaction IN (?" + strings.Repeat(", ?", len(keys)-1) + ")"
	args := []any{userId, mtproto.PEER_USER, userId, offsetId}
	for _, k := range keys {
		args = append(args, k)
	}
	if savedPeer != nil {
		query += " AND m.saved_peer_type = ? AND m.saved_peer_id = ?"
		args = append(args, savedPeer.PeerType, savedPeer.PeerId)
	}
	if text != "" {
		query += " AND m.message LIKE ?"
		args = append(args, "%"+likeEscaper.Replace(text)+"%")
	}
	query += " GROUP BY m.user_message_box_id HAVING COUNT(DISTINCT r.reaction) = ? ORDER BY m.user_message_box_id DESC LIMIT ?"
	args = append(args, len(keys), limit)
	var ids []int32
	err := q.QueryRowsPartial(ctx, &ids, query, args...)
	return ids, err
}

// likeEscaper escapes the wildcards of LIKE in a text searched for.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
