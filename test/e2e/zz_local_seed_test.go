//go:build e2e && seedlocal

package e2e

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestSeedLocal fills a local stand for checks in the clients. Not part of
// the suite: build tag seedlocal.
func TestSeedLocal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phone := func(i int) string { return fmt.Sprintf("+1201555%04d", 9000+i) }
	alice := login(ctx, t, phone(1), "Алиса")
	bob := login(ctx, t, phone(2), "Борис")
	vera := login(ctx, t, phone(3), "Вера")
	others := []*user{}
	for i, name := range []string{"Дан", "Ева", "Фаина", "Глеб", "Илья", "Кира", "Лев"} {
		others = append(others, login(ctx, t, phone(4+i), name))
	}

	today := time.Now().UTC()
	for _, u := range []*user{bob, vera} {
		if _, err := u.c.Invoke(ctx, &mtproto.TLAccountUpdateBirthday{
			Constructor: u.c.Constructor(mtproto.Predicate_account_updateBirthday),
			Birthday:    mtproto.MakeTLBirthday(&mtproto.Birthday{Month: int32(today.Month()), Day: int32(today.Day())}).To_Birthday(),
		}); err != nil {
			t.Fatalf("account.updateBirthday: %v", err)
		}
	}
	importContacts(ctx, t, alice,
		phoneContact(alice.c, 1, formattedPhone(bob.phone), "Борис", ""),
		phoneContact(alice.c, 2, formattedPhone(vera.phone), "Вера", ""))
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, formattedPhone(alice.phone), "Алиса", ""))
	importContacts(ctx, t, vera, phoneContact(vera.c, 1, formattedPhone(alice.phone), "Алиса", ""))

	// Mentions of Alice in a group.
	group := createChat(ctx, t, bob, "Проверка 020", alice, vera)
	for i := 1; i <= 3; i++ {
		if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        chatPeer(bob, group),
			Message:     fmt.Sprintf("Алиса, посмотри %d", i),
			RandomId:    randomID(),
			Entities: []*mtproto.MessageEntity{
				mtproto.MakeTLInputMessageEntityMentionName(&mtproto.MessageEntity{Offset: 0, Length: 5, UserId_INPUTUSER: inputUserOf(ctx, t, bob, alice)}).To_MessageEntity(),
			},
		}); err != nil {
			t.Fatalf("messages.sendMessage: %v", err)
		}
	}
	sendMessageID(ctx, t, vera, chatPeer(vera, group), "Просто сообщение без упоминания", randomID())

	// Photos and a GIF from Bob to Alice: the media tab and a GIF to save.
	toAlice := peerOf(ctx, t, bob, alice)
	for range 3 {
		sendPhoto(ctx, t, bob, toAlice)
	}
	sendMessageID(ctx, t, bob, toAlice, "Привет! Это Борис", randomID())
	sendGif(ctx, t, alice) // a GIF in Alice's Saved Messages, to save

	// Everybody in one group, for the identity colours of names and avatars.
	members := append([]*user{alice, bob, vera}, others...)
	colours := createChat(ctx, t, alice, "Цвета", members[1:]...)
	for _, u := range members {
		sendMessageID(ctx, t, u, chatPeer(u, colours), fmt.Sprintf("Я %s, мой цвет %d", u.self.GetFirstName().GetValue(), u.self.GetId()%7), randomID())
	}
	for _, u := range members {
		t.Logf("%s %s id %d colour %d", u.phone, u.self.GetFirstName().GetValue(), u.self.GetId(), u.self.GetId()%7)
	}
}

func TestSeedLocalFullUser(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, "+12015559001", "Алиса")
	bob := login(ctx, t, "+12015559002", "Борис")
	full, u := fullUser(ctx, t, alice, inputUserOf(ctx, t, alice, bob))
	t.Logf("birthday %v contact %v mutual %v", full.GetBirthday(), u.GetContact(), u.GetMutualContact())
	self, _ := fullUser(ctx, t, bob, inputSelf(bob))
	t.Logf("Bob's own birthday %v", self.GetBirthday())
}

// TestSeedLocalBirthdayContacts gives Vera two contacts with a birthday today
// (Alice and Boris) and Gleb one (Alice), for the birthday banner.
func TestSeedLocalBirthdayContacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, "+12015559001", "Алиса")
	bob := login(ctx, t, "+12015559002", "Борис")
	vera := login(ctx, t, "+12015559003", "Вера")
	gleb := login(ctx, t, "+12015559007", "Глеб")
	importContacts(ctx, t, vera, phoneContact(vera.c, 1, formattedPhone(bob.phone), "Борис", ""))
	importContacts(ctx, t, bob, phoneContact(bob.c, 1, formattedPhone(vera.phone), "Вера", ""))
	importContacts(ctx, t, gleb, phoneContact(gleb.c, 1, formattedPhone(alice.phone), "Алиса", ""))
	importContacts(ctx, t, alice, phoneContact(alice.c, 1, formattedPhone(gleb.phone), "Глеб", ""))
}

// TestSeedLocalDialogsUsers prints the users of Vera's dialogs.
func TestSeedLocalDialogsUsers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	vera := login(ctx, t, "+12015559003", "Вера")
	o, err := vera.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
		Constructor: vera.c.Constructor(mtproto.Predicate_messages_getDialogs),
		OffsetPeer:  &mtproto.InputPeer{Constructor: vera.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       20,
	})
	if err != nil {
		t.Fatal(err)
	}
	d := o.(interface {
		To_Messages_Dialogs() *mtproto.Messages_Dialogs
	}).To_Messages_Dialogs()
	for _, m := range d.GetMessages() {
		t.Logf("message %d in %v from %v", m.GetId(), m.GetPeerId(), m.GetFromId())
	}
	for _, u := range d.GetUsers() {
		t.Logf("user %d %s %q deleted=%v min=%v", u.GetId(), u.GetPredicateName(), u.GetFirstName().GetValue(), u.GetDeleted(), u.GetMin())
	}
}

// TestSeedLocalOtherChatReplies shows Gleb replies to messages of chats he
// has no part in: in a new group "Ответы", Alice answers a photo Bob sent
// her; in her private chat with Gleb, she quotes Bob from "Проверка 020".
func TestSeedLocalOtherChatReplies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, "+12015559001", "Алиса")
	bob := login(ctx, t, "+12015559002", "Борис")
	vera := login(ctx, t, "+12015559003", "Вера")
	gleb := login(ctx, t, "+12015559007", "Глеб")

	caption := "Вот так выглядит перо на закате"
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:        peerOf(ctx, t, bob, alice),
		Media:       uploadPhoto(ctx, t, bob),
		Message:     caption,
		RandomId:    randomID(),
	}); err != nil {
		t.Fatalf("messages.sendMedia: %v", err)
	}
	toBob := peerOf(ctx, t, alice, bob)
	photo, _ := historyMessage(ctx, t, alice, toBob, caption)

	group := createChat(ctx, t, alice, "Ответы", gleb, vera)
	if _, err := sendWithReply(ctx, alice, chatPeer(alice, group), "Смотрите, что прислал Борис", mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId: photo.GetId(), ReplyToPeerId: toBob,
	}).To_InputReplyTo()); err != nil {
		t.Fatalf("reply to the photo: %v", err)
	}

	said := "Алиса, посмотри 2"
	var check *mtproto.Message
	for _, d := range []string{said} {
		dialogs, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSearchGlobal{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_searchGlobal),
			Q:           d,
			Filter:      &mtproto.MessagesFilter{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessagesFilterEmpty)},
			OffsetPeer:  &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
			Limit:       5,
		})
		if err != nil {
			t.Fatalf("messages.searchGlobal: %v", err)
		}
		for _, m := range dialogs.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			if m.GetMessage() == d {
				check = m
			}
		}
	}
	if check == nil {
		t.Fatalf("no message %q in Alice's chats", said)
	}
	quote := "посмотри"
	toGleb := peerOf(ctx, t, alice, gleb)
	if _, err := sendWithReply(ctx, alice, toGleb, "Борис просил тебя тоже", mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId:  check.GetId(),
		ReplyToPeerId: &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChat), ChatId: check.GetPeerId().GetChatId()},
		QuoteText:     &wrapperspb.StringValue{Value: quote},
		QuoteOffset:   &wrapperspb.Int32Value{Value: 7},
	}).To_InputReplyTo()); err != nil {
		t.Fatalf("reply with a quote: %v", err)
	}
}

// TestSeedLocalOtherChatRepliesForDan shows Dan replies to messages of
// chats he has no part in: Vera answers him in private with Alice's message
// of "Ответы"; Alice answers in "Цвета" the photo Boris sent her.
func TestSeedLocalOtherChatRepliesForDan(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	alice := login(ctx, t, "+12015559001", "Алиса")
	vera := login(ctx, t, "+12015559003", "Вера")
	dan := login(ctx, t, "+12015559004", "Дан")

	find := func(u *user, text string) *mtproto.Message {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSearchGlobal{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_searchGlobal),
			Q:           text,
			Filter:      &mtproto.MessagesFilter{Constructor: u.c.Constructor(mtproto.Predicate_inputMessagesFilterEmpty)},
			OffsetPeer:  &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
			Limit:       10,
		})
		if err != nil {
			t.Fatalf("messages.searchGlobal: %v", err)
		}
		for _, m := range o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			if strings.HasPrefix(m.GetMessage(), text) {
				return m
			}
		}
		t.Fatalf("no message %q", text)
		return nil
	}
	chatOf := func(u *user, m *mtproto.Message) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChat), ChatId: m.GetPeerId().GetChatId()}
	}

	inAnswers := find(vera, "Смотрите, что прислал Борис")
	if os.Getenv("SEED_VERA") != "" {
		if _, err := sendWithReply(ctx, vera, peerOf(ctx, t, vera, dan), "Посмотри, что Алиса показала в нашей группе", mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
			ReplyToMsgId: inAnswers.GetId(), ReplyToPeerId: chatOf(vera, inAnswers),
		}).To_InputReplyTo()); err != nil {
			t.Fatalf("Vera's reply: %v", err)
		}
	}

	photo := find(alice, "Вот так выглядит перо на закате")
	colours := find(alice, "Я Алиса, мой цвет")
	toBob := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: photo.GetPeerId().GetUserId()}
	if _, err := sendWithReply(ctx, alice, chatOf(alice, colours), "Борис прислал мне это фото", mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId: photo.GetId(), ReplyToPeerId: toBob,
	}).To_InputReplyTo()); err != nil {
		t.Fatalf("Alice's reply: %v", err)
	}
}
