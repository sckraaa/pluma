//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// A secret chat through the public port (task 069): the server sees only
// public values and ciphertext, so the "ciphertext" here is arbitrary bytes.

// secretG is the public value g^x mod p for a random x, as the clients send.
func secretG(t *testing.T, c *mtproto.TLMessagesDhConfig) []byte {
	t.Helper()
	x := make([]byte, 256)
	if _, err := rand.Read(x); err != nil {
		t.Fatal(err)
	}
	p := new(big.Int).SetBytes(c.GetP())
	v := new(big.Int).Exp(big.NewInt(int64(c.GetG())), new(big.Int).SetBytes(x), p)
	return v.FillBytes(make([]byte, 256))
}

func dhConfig(ctx context.Context, t *testing.T, u *user, version int32) mtproto.TLObject {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetDhConfig{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getDhConfig), Version: version, RandomLength: 256,
	})
	if err != nil {
		t.Fatalf("messages.getDhConfig: %v", err)
	}
	return o
}

func encryptionUpdate(ctx context.Context, t *testing.T, u *user, chatId int32, predicate string) *mtproto.EncryptedChat {
	t.Helper()
	var got *mtproto.EncryptedChat
	if _, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, up := range us.GetUpdates() {
			if up.GetPredicateName() == mtproto.Predicate_updateEncryption && up.GetChat().GetPredicateName() == predicate && (chatId == 0 || up.GetChat().GetId() == chatId) {
				got = up.GetChat()
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("%s: %v", predicate, err)
	}
	return got
}

func newEncryptedMessage(ctx context.Context, t *testing.T, u *user, randomId int64) (*mtproto.EncryptedMessage, int32) {
	t.Helper()
	var (
		m   *mtproto.EncryptedMessage
		qts int32
	)
	if _, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, up := range us.GetUpdates() {
			if up.GetPredicateName() == mtproto.Predicate_updateNewEncryptedMessage && up.GetMessage_ENCRYPTEDMESSAGE().GetRandomId() == randomId {
				m, qts = up.GetMessage_ENCRYPTEDMESSAGE(), up.GetQts()
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("updateNewEncryptedMessage: %v", err)
	}
	return m, qts
}

func wantRPCError(t *testing.T, err error, msg string) {
	t.Helper()
	var e *mtclient.RPCError
	if !errors.As(err, &e) || e.Message != msg {
		t.Fatalf("got %v, want %s", err, msg)
	}
}

func TestSecretChat(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	bob2 := login(ctx, t, phones[1], "Bob second")
	for _, u := range []*user{alice, bob, bob2} {
		getState(ctx, t, u)
	}
	toBob := peerOf(ctx, t, alice, bob)

	// DH: the first answer has the parameters, the next only random bytes.
	cfg, ok := dhConfig(ctx, t, alice, 0).(*mtproto.TLMessagesDhConfig)
	if !ok || cfg.GetVersion() == 0 || len(cfg.GetP()) != 256 || len(cfg.GetRandom()) != 256 {
		t.Fatalf("dhConfig: %v", cfg)
	}
	if nm, ok := dhConfig(ctx, t, alice, cfg.GetVersion()).(*mtproto.TLMessagesDhConfigNotModified); !ok || len(nm.GetRandom()) != 256 {
		t.Fatalf("not modified: %v", nm)
	}

	// The request goes to every device of the participant.
	gA := secretG(t, cfg)
	input := &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUser, Constructor: alice.c.Constructor(mtproto.Predicate_inputUser), UserId: toBob.GetUserId(), AccessHash: toBob.GetAccessHash()}
	reqId := int32(randomID())
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestEncryption{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestEncryption), UserId: input, RandomId: reqId, GA: gA,
	})
	if err != nil {
		t.Fatalf("requestEncryption: %v", err)
	}
	waiting, ok := o.(*mtproto.TLEncryptedChatWaiting)
	if !ok || waiting.GetId() == 0 || waiting.GetAdminId() != alice.self.GetId() || waiting.GetParticipantId() != bob.self.GetId() {
		t.Fatalf("waiting: %T %v", o, o)
	}
	chatId, accessHash := waiting.GetId(), waiting.GetAccessHash()
	req := encryptionUpdate(ctx, t, bob, chatId, mtproto.Predicate_encryptedChatRequested)
	if req.GetAccessHash() != accessHash || !bytes.Equal(req.GetGA(), gA) {
		t.Fatalf("requested: %v", req)
	}
	encryptionUpdate(ctx, t, bob2, chatId, mtproto.Predicate_encryptedChatRequested)

	// A repeated request is the same chat, no new push.
	o2, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestEncryption{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestEncryption), UserId: input, RandomId: reqId, GA: gA,
	})
	if err != nil || o2.(*mtproto.TLEncryptedChatWaiting).GetId() != chatId {
		t.Fatalf("repeated request: %v %v", o2, err)
	}

	// Bob's first device accepts: Alice gets the key, the other device loses it.
	peerOfBob := mtproto.MakeTLInputEncryptedChat(&mtproto.InputEncryptedChat{ChatId: chatId, AccessHash: accessHash}).To_InputEncryptedChat()
	gB := secretG(t, cfg)
	const fingerprint = int64(0x1122334455667788)
	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesAcceptEncryption{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_acceptEncryption), Peer: peerOfBob, GB: gB, KeyFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("acceptEncryption: %v", err)
	}
	accepted, ok := o.(*mtproto.TLEncryptedChat)
	if !ok || !bytes.Equal(accepted.GetGAOrB(), gA) || accepted.GetKeyFingerprint() != fingerprint {
		t.Fatalf("accepted: %T %v", o, o)
	}
	a := encryptionUpdate(ctx, t, alice, chatId, mtproto.Predicate_encryptedChat)
	if !bytes.Equal(a.GetGAOrB(), gB) || a.GetKeyFingerprint() != fingerprint {
		t.Fatalf("alice: %v", a)
	}
	encryptionUpdate(ctx, t, bob2, chatId, mtproto.Predicate_encryptedChatDiscarded)
	if _, err = bob2.c.Invoke(ctx, &mtproto.TLMessagesAcceptEncryption{
		Constructor: bob2.c.Constructor(mtproto.Predicate_messages_acceptEncryption), Peer: peerOfBob, GB: secretG(t, cfg), KeyFingerprint: 1,
	}); err == nil {
		t.Fatal("the other device accepted")
	} else {
		wantRPCError(t, err, "ENCRYPTION_ALREADY_ACCEPTED")
	}

	// A message reaches the accepting device with qts 2, the first after the
	// state of a new authorization; a repeat answers
	// the same date.
	first := randomID()
	sendMessage := func(rand int64, data []byte) *mtproto.TLMessagesSentEncryptedMessage {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendEncrypted{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendEncrypted),
			Peer:        peerOfBob,
			RandomId:    rand,
			Data:        data,
		})
		if err != nil {
			t.Fatalf("sendEncrypted: %v", err)
		}
		return o.(*mtproto.TLMessagesSentEncryptedMessage)
	}
	sent := sendMessage(first, []byte("ciphertext one"))
	m, qts := newEncryptedMessage(ctx, t, bob, first)
	if m.GetChatId() != chatId || !bytes.Equal(m.GetBytes(), []byte("ciphertext one")) || qts != 2 {
		t.Fatalf("message: qts %d %v", qts, m)
	}
	if again := sendMessage(first, []byte("ciphertext one")); again.GetDate() != sent.GetDate() {
		t.Fatalf("repeat date %d != %d", again.GetDate(), sent.GetDate())
	}

	// Without a push the message is in the difference of the queue; the state
	// follows, and the qts given acknowledges.
	second := randomID()
	sendMessage(second, []byte("ciphertext two"))
	newEncryptedMessage(ctx, t, bob, second)
	st := getState(ctx, t, bob)
	if st.GetQts() != 3 {
		t.Fatalf("qts of the state %d", st.GetQts())
	}
	o, err = bob.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
		Constructor: bob.c.Constructor(mtproto.Predicate_updates_getDifference), Pts: st.GetPts(), Date: st.GetDate(), Qts: 2,
	})
	if err != nil {
		t.Fatalf("getDifference: %v", err)
	}
	d := o.(interface {
		To_Updates_Difference() *mtproto.Updates_Difference
	}).To_Updates_Difference()
	if len(d.GetNewEncryptedMessages()) != 1 || d.GetNewEncryptedMessages()[0].GetRandomId() != second || d.GetState().GetQts() != 3 {
		t.Fatalf("difference: %v", d)
	}

	// An encrypted file: stored and downloaded by the receiver.
	fileId := randomID()
	content := []byte("encrypted file content, opaque to the server")
	if _, err = alice.c.Invoke(ctx, &mtproto.TLUploadSaveFilePart{
		Constructor: alice.c.Constructor(mtproto.Predicate_upload_saveFilePart), FileId: fileId, FilePart: 0, Bytes: content,
	}); err != nil {
		t.Fatalf("saveFilePart: %v", err)
	}
	withFile := randomID()
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendEncryptedFile{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendEncryptedFile),
		Peer:        peerOfBob,
		RandomId:    withFile,
		Data:        []byte("caption"),
		File:        mtproto.MakeTLInputEncryptedFileUploaded(&mtproto.InputEncryptedFile{Id: fileId, Parts: 1, KeyFingerprint: 11}).To_InputEncryptedFile(),
	})
	if err != nil {
		t.Fatalf("sendEncryptedFile: %v", err)
	}
	sentFile := o.(*mtproto.TLMessagesSentEncryptedFile).GetFile()
	if sentFile.GetKeyFingerprint() != 11 || sentFile.GetId() == 0 {
		t.Fatalf("sent file: %v", sentFile)
	}
	withFileMsg, _ := newEncryptedMessage(ctx, t, bob, withFile)
	file := withFileMsg.GetFile()
	if file.GetId() != sentFile.GetId() || file.GetAccessHash() != sentFile.GetAccessHash() {
		t.Fatalf("received file: %v", file)
	}
	location := func(hash int64) *mtproto.InputFileLocation {
		return mtproto.MakeTLInputEncryptedFileLocation(&mtproto.InputFileLocation{Id: file.GetId(), AccessHash: hash}).To_InputFileLocation()
	}
	getFile := func(hash int64) (*mtproto.TLUploadFile, error) {
		var last error
		for i := 0; i < 50; i++ {
			o, err := bob.c.Invoke(ctx, &mtproto.TLUploadGetFile{
				Constructor: bob.c.Constructor(mtproto.Predicate_upload_getFile),
				Location:    location(hash),
				Limit:       1 << 18,
			})
			if err != nil {
				return nil, err
			}
			f := o.(*mtproto.TLUploadFile)
			if len(f.GetBytes()) != 0 {
				return f, nil
			}
			last = errors.New("empty file")
			time.Sleep(100 * time.Millisecond)
		}
		return nil, last
	}
	downloaded, err := getFile(file.GetAccessHash())
	if err != nil || !bytes.Equal(downloaded.GetBytes(), content) {
		t.Fatalf("download: %v %v", downloaded, err)
	}
	if _, err = getFile(file.GetAccessHash() + 1); err == nil {
		t.Fatal("a wrong access hash downloads the file")
	} else {
		wantRPCError(t, err, "FILE_ID_INVALID")
	}

	// A read reaches the other end only.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesReadEncryptedHistory{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_readEncryptedHistory), Peer: peerOfBob, MaxDate: sent.GetDate(),
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err = alice.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, up := range us.GetUpdates() {
			if up.GetPredicateName() == mtproto.Predicate_updateEncryptedMessagesRead && up.GetChatId_INT32() == chatId && up.GetMaxDate() == sent.GetDate() {
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("read update: %v", err)
	}

	// messages.receivedQueue returns what it confirms, after that nothing.
	rq := func() int {
		t.Helper()
		raw, err := bob.c.InvokeRaw(ctx, &mtproto.TLMessagesReceivedQueue{
			Constructor: bob.c.Constructor(mtproto.Predicate_messages_receivedQueue), MaxQts: 4,
		})
		if err != nil || len(raw) < 8 {
			t.Fatalf("receivedQueue: %v (%d bytes)", err, len(raw))
		}
		return int(binary.LittleEndian.Uint32(raw[4:]))
	}
	// The messages 2 and 3; the first went with the qts of the difference.
	if n := rq(); n != 2 {
		t.Fatalf("receivedQueue confirmed %d messages", n)
	}
	if n := rq(); n != 0 {
		t.Fatalf("receivedQueue confirmed %d messages again", n)
	}

	// A second device of the same account is refused.
	if _, err = bob2.c.Invoke(ctx, &mtproto.TLMessagesSendEncrypted{
		Constructor: bob2.c.Constructor(mtproto.Predicate_messages_sendEncrypted), Peer: peerOfBob, RandomId: randomID(), Data: []byte("x"),
	}); err == nil {
		t.Fatal("another device sent")
	} else {
		wantRPCError(t, err, "ENCRYPTION_ID_INVALID")
	}

	// Discard with the history: Alice is told, and sends no more.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesDiscardEncryption{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_discardEncryption), DeleteHistory: true, ChatId: chatId,
	}); err != nil {
		t.Fatalf("discard: %v", err)
	}
	dis := encryptionUpdate(ctx, t, alice, chatId, mtproto.Predicate_encryptedChatDiscarded)
	if !dis.GetHistoryDeleted() {
		t.Fatalf("discarded without history_deleted: %v", dis)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendEncrypted{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendEncrypted), Peer: peerOfBob, RandomId: randomID(), Data: []byte("x"),
	}); err == nil {
		t.Fatal("sent to a discarded chat")
	} else {
		wantRPCError(t, err, "ENCRYPTION_DECLINED")
	}
}

// TestSecretChatBlocked: a blocked user gets USER_IS_BLOCKED for a request.
func TestSecretChatBlocked(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	toBob := peerOf(ctx, t, alice, bob)
	toAlice := peerOf(ctx, t, bob, alice)
	if _, err := bob.c.Invoke(ctx, &mtproto.TLContactsBlock{Constructor: bob.c.Constructor(mtproto.Predicate_contacts_block), Id: toAlice}); err != nil {
		t.Fatalf("contacts.block: %v", err)
	}
	cfg := dhConfig(ctx, t, alice, 0).(*mtproto.TLMessagesDhConfig)
	input := &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUser, Constructor: alice.c.Constructor(mtproto.Predicate_inputUser), UserId: toBob.GetUserId(), AccessHash: toBob.GetAccessHash()}
	_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestEncryption{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestEncryption), UserId: input, RandomId: int32(randomID()), GA: secretG(t, cfg),
	})
	wantRPCError(t, err, "USER_IS_BLOCKED")
}
