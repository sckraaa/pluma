package config

import (
	"os"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/conf"
)

// TestDeployConfigs loads the shipped gnetway configs the way the service
// does and checks where they take the server RSA key from.
func TestDeployConfigs(t *testing.T) {
	tests := []struct {
		file    string
		keyFile string
	}{
		{"../../../../../teamgramd/etc2/gnetway.yaml", "/run/secrets/server_pkcs1.key"},
		{"../../../../../teamgramd/etc/gnetway.yaml", "../../secrets/server_pkcs1.key"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			os.Unsetenv("SERVER_RSA_KEY_FINGERPRINT")
			var c Config
			err := conf.Load(tt.file, &c)
			if err == nil || !strings.Contains(err.Error(), "SERVER_RSA_KEY_FINGERPRINT") {
				t.Fatalf("load without SERVER_RSA_KEY_FINGERPRINT: error = %v, want it named", err)
			}

			t.Setenv("SERVER_RSA_KEY_FINGERPRINT", "0xa9e071c1771060cd")
			c = Config{}
			if err = conf.Load(tt.file, &c); err != nil {
				t.Fatal(err)
			}
			if len(c.RSAKey) != 1 {
				t.Fatalf("RSAKey = %+v, want one key", c.RSAKey)
			}
			if c.RSAKey[0].KeyFile != tt.keyFile || c.RSAKey[0].KeyFingerprint != "0xa9e071c1771060cd" {
				t.Fatalf("RSAKey[0] = %+v", c.RSAKey[0])
			}
		})
	}
}
