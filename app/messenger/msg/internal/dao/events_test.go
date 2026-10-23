package dao

import (
	"errors"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// inboxEvents are the event types of the inbox schema, found by the schema
// itself so that a new event cannot be left out.
func inboxEvents(t *testing.T) []protoreflect.MessageType {
	t.Helper()
	fd := inbox.File_inbox_tl_proto
	var out []protoreflect.MessageType
	for i := range fd.Messages().Len() {
		md := fd.Messages().Get(i)
		if !strings.HasPrefix(string(md.Name()), "TL_inbox_") {
			continue
		}
		mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, mt)
	}
	if len(out) == 0 {
		t.Fatal("no inbox events")
	}
	return out
}

// Every inbox event is addressed to one user, its user_id, which is the
// Kafka key: the events of one recipient keep their order in one partition.
func TestInboxEventKeyIsRecipient(t *testing.T) {
	d := &Dao{Topics: Topics{Inbox: "Inbox-T", Sync: "Sync-T"}}
	for _, mt := range inboxEvents(t) {
		name := mt.Descriptor().FullName()
		t.Run(string(name), func(t *testing.T) {
			m := mt.New()
			fields := m.Descriptor().Fields()
			userId, fromId := fields.ByName("user_id"), fields.ByName("from_id")
			if userId == nil || fromId == nil {
				t.Fatalf("%s has no user_id or from_id", name)
			}
			m.Set(userId, protoreflect.ValueOfInt64(7))
			m.Set(fromId, protoreflect.ValueOfInt64(3))
			in, ok := m.Interface().(fromEvent)
			if !ok {
				t.Fatalf("%s is not an inbox event", name)
			}

			ev, err := d.InboxEvent(in)
			if err != nil {
				t.Fatal(err)
			}
			if ev.Topic != "Inbox-T" || ev.Recipient != 7 || ev.Method != string(name) {
				t.Errorf("event %+v", ev)
			}

			b := &userbox.Box{}
			if err = d.EmitInbox(b, in); err != nil {
				t.Fatal(err)
			}

			// An event to its initiator is refused.
			m.Set(fromId, protoreflect.ValueOfInt64(7))
			if _, err = d.InboxEvent(in); !errors.Is(err, errSelfEvent) {
				t.Errorf("event to its initiator: %v", err)
			}
		})
	}
}
