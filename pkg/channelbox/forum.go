package channelbox

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// GeneralTopicId is the id of a forum's General topic
// (core.telegram.org/api/forum); its messages have topic_id 0.
const GeneralTopicId = 1

// TopicColumn is the value of channel_messages.topic_id for a topic.
func TopicColumn(topicId int32) int32 {
	if topicId == GeneralTopicId {
		return 0
	}
	return topicId
}

// ForumMoved records message last of b's owner in a topic of the forum:
// the channel's and the topic's top message move, and the owner has read
// the topic up to it. Unlike a supergroup's dialog, the owner's
// read_inbox_max_id stays: it reads the forum as a whole, and moving it
// would read every other topic too.
func ForumMoved(b *userbox.Box, channelId int64, topicId, last int32) error {
	if _, err := b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", last, channelId); err != nil {
		return err
	}
	if _, err := b.Tx().Exec("UPDATE channel_forum_topics SET top_message = ? WHERE channel_id = ? AND topic_id = ?", last, channelId, topicId); err != nil {
		return err
	}
	if _, err := b.Tx().Exec("UPDATE dialogs SET top_message = ?, date2 = ?, deleted = 0 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		last, b.Date(), b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return err
	}
	_, err := b.Tx().Exec("INSERT INTO channel_thread_reads (user_id, channel_id, top_id, read_max_id) VALUES (?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE read_max_id = GREATEST(read_max_id, VALUES(read_max_id))",
		b.UserId(), channelId, topicId, last)
	return err
}
