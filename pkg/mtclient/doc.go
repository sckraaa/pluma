// Package mtclient is an MTProto 2.0 client for tests: it talks to the server
// through its public port the way the official clients do, so end-to-end and
// conformance tests (task 007) can drive the whole stack.
//
// It covers the transport (intermediate over TCP), the key exchange
// (permanent and temporary keys, with the checks the protocol requires of a
// client), message encryption, msg_id/seqno, containers, acknowledgements,
// ignoring repeated server messages (counted by Duplicates), gzip_packed,
// re-sending after bad_server_salt and bad_msg_notification,
// rpc_result/rpc_error, pushed updates and auth.bindTempAuthKey. TL types and
// the AES-IGE/AuthKey primitives come from github.com/teamgram/proto (the
// vendored third_party/teamgram-proto).
//
// A typical test:
//
//	c, err := mtclient.Dial(ctx, mtclient.Config{Addr: addr, PublicKey: pub})
//	// key exchange, session and initConnection are done
//	o, err := c.Invoke(ctx, &mtproto.TLAuthSendCode{
//		Constructor: c.Constructor(mtproto.Predicate_auth_sendCode), ...})
//	u, err := c.WaitUpdate(ctx, func(u *mtproto.Updates) bool { ... })
//
// The client logs (Config.Logf) TL types and message ids only, never the
// content of messages, keys or codes.
package mtclient
