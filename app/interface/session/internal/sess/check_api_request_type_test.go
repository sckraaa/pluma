package sess

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func TestCheckRpcWithoutLogin(t *testing.T) {
	tests := []struct {
		name string
		tl   mtproto.TLObject
		want bool
	}{
		{"auth.sendCode", &mtproto.TLAuthSendCode{}, true},
		{"auth.signIn", &mtproto.TLAuthSignIn{}, true},
		{"auth.signUp", &mtproto.TLAuthSignUp{}, true},
		{"auth.resendCode", &mtproto.TLAuthResendCode{}, true},
		{"auth.cancelCode", &mtproto.TLAuthCancelCode{}, true},
		{"help.getConfig", &mtproto.TLHelpGetConfig{}, true},
		{"langpack.getLangPack", &mtproto.TLLangpackGetLangPack{}, true},
		// Finding #23: files are not served before login.
		{"upload.getFile", &mtproto.TLUploadGetFile{}, false},
		{"upload.getWebFile", &mtproto.TLUploadGetWebFile{}, false},
		{"messages.getDialogs", &mtproto.TLMessagesGetDialogs{}, false},
	}
	for _, tt := range tests {
		if got := checkRpcWithoutLogin(tt.tl); got != tt.want {
			t.Errorf("%s: checkRpcWithoutLogin = %v, want %v", tt.name, got, tt.want)
		}
	}
}
