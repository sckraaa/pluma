package core

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/chats/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
)

// portionMsg answers msg.deleteHistory with the offsets of its portions, in
// turn, or with err.
type portionMsg struct {
	msg_client.MsgClient
	offsets []int32
	err     error
	calls   []*msgpb.TLMsgDeleteHistory
}

func (m *portionMsg) MsgDeleteHistory(_ context.Context, in *msgpb.TLMsgDeleteHistory) (*mtproto.Messages_AffectedHistory, error) {
	m.calls = append(m.calls, in)
	if m.err != nil {
		return nil, m.err
	}
	offset := m.offsets[0]
	m.offsets = m.offsets[1:]
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{Offset: offset}).To_Messages_AffectedHistory(), nil
}

func chatsCore(m msg_client.MsgClient) *ChatsCore {
	return New(context.Background(), &svc.ServiceContext{Dao: &dao.Dao{MsgClient: m}})
}

// A member's history of a deleted chat goes in as many portions as msg
// asks for, the whole of it and the dialog row with the last one.
func TestDeleteChatHistoryPortions(t *testing.T) {
	m := &portionMsg{offsets: []int32{1500, 500, 0}}
	if err := chatsCore(m).deleteChatHistory(7, 42); err != nil {
		t.Fatal(err)
	}
	if len(m.calls) != 3 {
		t.Fatalf("%d calls, want 3", len(m.calls))
	}
	for _, in := range m.calls {
		if in.UserId != 7 || in.PeerType != mtproto.PEER_CHAT || in.PeerId != 42 || in.JustClear || in.Revoke {
			t.Errorf("call %+v", in)
		}
	}
}

func TestDeleteChatHistoryError(t *testing.T) {
	busy := errors.New("user busy")
	m := &portionMsg{err: busy}
	if err := chatsCore(m).deleteChatHistory(7, 42); !errors.Is(err, busy) {
		t.Fatalf("error %v, want %v", err, busy)
	}
}
