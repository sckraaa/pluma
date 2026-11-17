package config

import (
	"os"
	"strings"
	"testing"
	"time"

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
			if err := c.Gnetway.Validate(); err != nil {
				t.Fatal(err)
			}
			if !c.Gnetway.IsEdge("0.0.0.0:11445") || !c.Gnetway.IsWebsocket("0.0.0.0:11445") || c.Gnetway.IsProxyProtocolV1("0.0.0.0:11445") {
				t.Fatal("no Edge WebSocket listener on 11445")
			}
			if c.Gnetway.IsEdge("0.0.0.0:11443") || c.Gnetway.IsEdge("0.0.0.0:11444") || c.Gnetway.IsEdge("0.0.0.0:10443") {
				t.Fatal("another listener is Edge")
			}
			if c.Gnetway.EdgePingInterval != 20*time.Second {
				t.Fatalf("EdgePingInterval = %s, want the default 20s", c.Gnetway.EdgePingInterval)
			}
			if got := strings.Join(c.Gnetway.EdgeViaNames(), ","); got != "gcore,ru" {
				t.Fatalf("EdgeVias = %s, want the default gcore,ru", got)
			}
		})
	}
}

// TestValidateEdge: an Edge listener is a WebSocket one without PROXY
// protocol, the ping interval stays within 5s..25s (below the 30 s Gcore
// keeps an idle WebSocket), and EdgeVias are distinct names.
func TestValidateEdge(t *testing.T) {
	edge := GnetwayServer{Proto: "websocket", Edge: true, Addresses: []string{"0.0.0.0:11445"}}
	valid := func() GnetwayConfig {
		return GnetwayConfig{
			Server: []GnetwayServer{
				{Proto: "tcp", Addresses: []string{"0.0.0.0:10443"}},
				{Proto: "websocket", PPV1: true, Addresses: []string{"0.0.0.0:11444"}},
				edge,
			},
			EdgePingInterval: 20 * time.Second,
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("a valid config: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(c *GnetwayConfig)
		want   string
	}{
		{"Edge and PPV1", func(c *GnetwayConfig) { c.Server[2].PPV1 = true }, "both Edge and PPV1"},
		{"Edge over TCP", func(c *GnetwayConfig) { c.Server[2].Proto = "tcp" }, "must be Proto websocket"},
		{"ping too rare", func(c *GnetwayConfig) { c.EdgePingInterval = 30 * time.Second }, "EdgePingInterval"},
		{"ping too often", func(c *GnetwayConfig) { c.EdgePingInterval = time.Second }, "EdgePingInterval"},
		{"no ping interval", func(c *GnetwayConfig) { c.EdgePingInterval = 0 }, "EdgePingInterval"},
		{"via other", func(c *GnetwayConfig) { c.EdgeVias = []string{"gcore", "other"} }, `"other"`},
		{"via upper case", func(c *GnetwayConfig) { c.EdgeVias = []string{"Gcore"} }, `"Gcore"`},
		{"via too long", func(c *GnetwayConfig) { c.EdgeVias = []string{strings.Repeat("a", 17)} }, "1-16"},
		{"via twice", func(c *GnetwayConfig) { c.EdgeVias = []string{"ru", "ru"} }, "twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.mutate(&c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() = %v, want an error with %q", err, tt.want)
			}
		})
	}
	c := valid()
	c.EdgeVias = []string{"gcore", "ru", "amf2"}
	if err := c.Validate(); err != nil {
		t.Fatalf("three edges: %v", err)
	}
}
