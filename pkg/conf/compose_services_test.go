// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type composeService struct {
	Image           string            `yaml:"image"`
	Command         []string          `yaml:"command"`
	Restart         string            `yaml:"restart"`
	WorkingDir      string            `yaml:"working_dir"`
	Environment     map[string]string `yaml:"environment"`
	Ports           []string          `yaml:"ports"`
	Secrets         []any             `yaml:"secrets"`
	Volumes         []string          `yaml:"volumes"`
	StopGracePeriod string            `yaml:"stop_grace_period"`
	Healthcheck     struct {
		Test []string `yaml:"test"`
	} `yaml:"healthcheck"`
}

// TestComposeServices keeps docker-compose.yaml at one process per service
// (ADR 0012, p. 8): each service runs its own binary in exec form with its
// own config, gets exactly the variables that config needs, drains within its
// stop_grace_period, and only gnetway publishes ports and gets the RSA key.
func TestComposeServices(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	// The services of the overlays added to the services command run from
	// the same image and configs, by the same rules.
	for _, f := range overlayComposeFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatal(err)
		}
		var overlay struct {
			Services map[string]composeService `yaml:"services"`
		}
		if err := yaml.Unmarshal(data, &overlay); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for name, svc := range overlay.Services {
			if _, ok := compose.Services[name]; ok {
				t.Errorf("%s: %s is a service of docker-compose.yaml already", f, name)
			}
			compose.Services[name] = svc
		}
	}

	configs, _ := filepath.Glob(filepath.Join(repoRoot, "teamgramd/etc2/*.yaml"))
	want := map[string]bool{}
	for _, f := range configs {
		want[strings.TrimSuffix(filepath.Base(f), ".yaml")] = true
	}
	for name := range want {
		if _, ok := compose.Services[name]; !ok {
			t.Errorf("teamgramd/etc2/%s.yaml has no compose service", name)
		}
	}

	varRe := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

	// The one-shot migrate service runs pluma-admin with the bff config
	// before everything else.
	mig, ok := compose.Services["migrate"]
	if !ok {
		t.Error("no migrate service")
	} else if mig.Restart != "no" || len(mig.Command) < 4 || mig.Command[0] != "./pluma-admin" || mig.Command[2] != "migrate" || mig.Command[3] != "up" {
		t.Errorf("migrate: command %q, restart %q", mig.Command, mig.Restart)
	}
	// storage-init prepares Garage with the dfs config before dfs starts.
	if si, ok := compose.Services["storage-init"]; !ok {
		t.Error("no storage-init service")
	} else if si.Restart != "no" || len(si.Command) < 5 || si.Command[0] != "./pluma-admin" || si.Command[1] != "-f=../etc2/dfs.yaml" || si.Command[2] != "storage" || si.Command[3] != "init" {
		t.Errorf("storage-init: command %q, restart %q", si.Command, si.Restart)
	}
	// The one-shot services load a whole service config: it must find
	// every variable of it.
	for _, name := range []string{"migrate", "storage-init"} {
		s := compose.Services[name]
		for _, arg := range s.Command {
			cfgName, ok := strings.CutPrefix(arg, "-f=../etc2/")
			if !ok {
				continue
			}
			cfg, err := os.ReadFile(filepath.Join(repoRoot, "teamgramd/etc2", cfgName))
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range varRe.FindAllStringSubmatch(string(cfg), -1) {
				if val, ok := s.Environment[m[1]]; !ok || !passed(m[1], val) {
					t.Errorf("%s loads %s, which uses ${%s}, but does not get it as a required variable", name, cfgName, m[1])
				}
			}
		}
		delete(compose.Services, name)
	}

	for name, s := range compose.Services {
		if !want[name] {
			t.Errorf("compose service %s has no config in teamgramd/etc2", name)
			continue
		}
		wantCmd := []string{"./" + name, "-f=../etc2/" + name + ".yaml"}
		if strings.Join(s.Command, " ") != strings.Join(wantCmd, " ") || s.WorkingDir != "/app/bin" {
			t.Errorf("%s: command %q in %q, want %q in /app/bin", name, s.Command, s.WorkingDir, wantCmd)
		}
		if s.Restart == "" || s.Image == "" {
			t.Errorf("%s: image %q, restart %q: both must come from the x-teamgram anchor", name, s.Image, s.Restart)
		}
		// The health check connects to the service's own port: its gRPC
		// ListenOn, or for sync (no gRPC server) its metrics port.
		port := listenPort(t, name)
		if want := "exec 3<>/dev/tcp/127.0.0.1/" + port; len(s.Healthcheck.Test) == 0 || s.Healthcheck.Test[len(s.Healthcheck.Test)-1] != want {
			t.Errorf("%s: healthcheck %q, want a connect to port %s", name, s.Healthcheck.Test, port)
		}
		grace, err := time.ParseDuration(s.StopGracePeriod)
		if err != nil || grace < 15*time.Second {
			t.Errorf("%s: stop_grace_period %q, want at least 15s", name, s.StopGracePeriod)
		}

		cfg, err := os.ReadFile(filepath.Join(repoRoot, "teamgramd/etc2", name+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		needed := map[string]bool{}
		for _, m := range varRe.FindAllStringSubmatch(string(cfg), -1) {
			needed[m[1]] = true
		}
		var got []string
		for v := range s.Environment {
			got = append(got, v)
			if !needed[v] {
				t.Errorf("%s gets %s, which its config does not use", name, v)
			}
		}
		sort.Strings(got)
		for v := range needed {
			val, ok := s.Environment[v]
			if !ok || !passed(v, val) {
				t.Errorf("%s: its config uses ${%s}, but the service does not get it as a required variable (has %v)", name, v, got)
			}
		}

		switch {
		case name == "gnetway":
			if len(s.Ports) == 0 || len(s.Secrets) == 0 {
				t.Errorf("gnetway must publish the client ports and get the RSA key")
			}
		case name == "push":
			// The provider keys, and nothing else (ADR 0017).
			if len(s.Ports) > 0 || fmt.Sprint(secretSources(s.Secrets)) != "[apns_key fcm_credentials]" {
				t.Errorf("push publishes ports %v or gets secrets %v; want only apns_key and fcm_credentials", s.Ports, s.Secrets)
			}
		case name == "links":
			// The keys of the ID tokens of oauth.pluma.chat (task 106), and
			// nothing else; its port is Caddy's, inside the network.
			if len(s.Ports) > 0 || fmt.Sprint(secretSources(s.Secrets)) != "[oauth_signing_key]" {
				t.Errorf("links publishes ports %v or gets secrets %v; want only oauth_signing_key", s.Ports, s.Secrets)
			}
		case name == "releases" && loopbackOnly(s.Ports) && len(s.Secrets) == 0:
			// The panel runs on the host and reaches it on the loopback
			// address (task 072).
		case len(s.Ports) > 0 || len(s.Secrets) > 0:
			t.Errorf("%s publishes ports %v or gets secrets %v: only gnetway, push and links may", name, s.Ports, s.Secrets)
		}
	}
}

// TestComposeGeoIP: authsession finds its GeoIP database where the
// deployment puts it, geoip/ at the root of the repository (/opt/pluma/geoip
// on a server, docs/ops/deployment.md). In Docker the directory is mounted
// read-only at the directory of GeoIPDatabase, and no other service gets it;
// on a bare-metal host the services run in teamgramd/bin.
func TestComposeGeoIP(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatal(err)
	}
	geoIPDatabase := func(config string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(repoRoot, config))
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			GeoIPDatabase string `yaml:"GeoIPDatabase"`
		}
		if err := yaml.Unmarshal(data, &c); err != nil {
			t.Fatal(err)
		}
		if c.GeoIPDatabase == "" {
			t.Fatalf("%s has no GeoIPDatabase", config)
		}
		return c.GeoIPDatabase
	}

	inContainer := geoIPDatabase("teamgramd/etc2/authsession.yaml")
	want := "./geoip:" + filepath.Dir(inContainer) + ":ro"
	for name, s := range compose.Services {
		for _, v := range s.Volumes {
			if strings.HasPrefix(v, "./geoip:") && (name != "authsession" || v != want) {
				t.Errorf("%s mounts %q; only authsession mounts %q", name, v, want)
			}
		}
	}
	if !slices.Contains(compose.Services["authsession"].Volumes, want) {
		t.Errorf("authsession volumes %q, want %q for GeoIPDatabase %s", compose.Services["authsession"].Volumes, want, inContainer)
	}

	if got := filepath.Join("teamgramd/bin", geoIPDatabase("teamgramd/etc/authsession.yaml")); filepath.Dir(got) != "geoip" {
		t.Errorf("teamgramd/etc/authsession.yaml: GeoIPDatabase is %s from teamgramd/bin, want a file in geoip/", got)
	}
}

// secretSources returns the sorted sources of a service's secrets.
func secretSources(secrets []any) []string {
	var out []string
	for _, v := range secrets {
		switch v := v.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			out = append(out, fmt.Sprint(v["source"]))
		}
	}
	sort.Strings(out)
	return out
}

// listenPort returns the port a service's health check connects to.
func listenPort(t *testing.T, name string) string {
	t.Helper()
	cfg, err := os.ReadFile(filepath.Join(repoRoot, "teamgramd/etc2", name+".yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		ListenOn   string `yaml:"ListenOn"`
		Prometheus struct {
			Port int `yaml:"Port"`
		} `yaml:"Prometheus"`
	}
	if err := yaml.Unmarshal(cfg, &c); err != nil {
		t.Fatal(err)
	}
	switch {
	case name == "gnetway":
		return "10443"
	case name == "sync" || name == "push": // no gRPC server is started (Sync-T consumers only)
		return strconv.Itoa(c.Prometheus.Port)
	case c.ListenOn != "":
		return c.ListenOn[strings.LastIndex(c.ListenOn, ":")+1:]
	}
	t.Fatalf("%s: no port to check", name)
	return ""
}

// overlayComposeFiles add services of the services image to
// docker-compose.yaml, each started only where its operator sets it up: the
// releases of the client apps (task 054, docs/ops/releases.md).
var overlayComposeFiles = []string{"docker-compose.releases.yaml"}

// optionalVars are the variables a service may get empty: settings that are
// off when empty, not secrets. Each of them is passed as ${NAME:-}.
var optionalVars = map[string]bool{
	// The App Store review account (pkg/logincode, task 029).
	"LOGIN_REVIEW_PHONE": true,
	"LOGIN_REVIEW_CODE":  true,
	// The service of login codes (task 073): none when empty.
	"LOGIN_CODES_SENDER_URL":   true,
	"LOGIN_CODES_SENDER_TOKEN": true,
	// The bot that fetches sticker sets from Telegram (pkg/tgbot, task
	// 030): no bot when empty.
	"TELEGRAM_API_ID":    true,
	"TELEGRAM_API_HASH":  true,
	"TELEGRAM_BOT_TOKEN": true,
	// The site of the client releases (task 054): bff offers no app
	// updates and no desktop update prefix when empty.
	"PLUMA_RELEASES_URL": true,
	// Its mirrors for the clients of some countries (task 113): the site
	// for everyone when empty.
	"PLUMA_RELEASES_ROUTES": true,
	// The caches of its /dl/ (task 115): the site and its mirrors when
	// empty.
	"PLUMA_RELEASES_FILES_URL":    true,
	"PLUMA_RELEASES_FILES_ROUTES": true,
	// The update of the iPhone app from the app (task 085): bff offers the
	// site's install page when empty.
	"PLUMA_IOS_UPDATE_SECRET": true,
	// The servers of one-to-one calls (task 068): calls are off when empty.
	"CALLS_ICE_SERVERS": true,
	"CALLS_TURN_SECRET": true,
	// The secrets of labelled TURN servers (task 109): none when empty.
	"CALLS_TURN_SECRETS": true,
	// The quality parameters of calls (task 077): the Pluma profile when
	// empty.
	"CALLS_QUALITY": true,
	// The control API of the SFU of group calls (task 093): group calls
	// are off when empty.
	"GROUPCALLS_SFU_ENDPOINT": true,
	// The speech engine of voice transcription (task 090): transcription
	// is off when the URL is empty.
	"STT_URL":      true,
	"STT_TOKEN":    true,
	"STT_MODEL":    true,
	"STT_LANGUAGE": true,
	"STT_TIMEOUT":  true,
	// The translation engine of messages (task 101): the translation is
	// off when the URL is empty.
	"TRANSLATE_URL":     true,
	"TRANSLATE_TOKEN":   true,
	"TRANSLATE_MODEL":   true,
	"TRANSLATE_PROFILE": true,
	"TRANSLATE_SLOTS":   true,
	"TRANSLATE_CTX":     true,
	// The mail of the recovery email of the cloud password (task 091):
	// off when the key is empty.
	"MAIL_API_KEY": true,
	"MAIL_FROM":    true,
	// The key of the web file proxy (task 099, ADR 0062): inline bots'
	// thumbnails go out unsigned, fetched by clients, when empty.
	"WEBFILE_HMAC_KEY": true,
	// The maps service of the map previews of geo points (task 100): maps
	// are off when the URL is empty, the rest take their defaults.
	"MAPS_URL":                 true,
	"MAPS_STYLES":              true,
	"MAPS_TIMEOUT":             true,
	"MAPS_CONCURRENCY":         true,
	"MAPS_CACHE_TTL":           true,
	"MAPS_PER_USER_PER_MINUTE": true,
	// The public map tiles of the clients' maps (task 112): none when empty.
	"MAPS_PUBLIC_URL": true,
	// The edges of the patched clients (task 109): none when empty, the
	// rest take their defaults.
	"EDGE_WS_URLS":  true,
	"EDGE_ROUTES":   true,
	"EDGE_STICKY_S": true,
	// The settings of the path probes of the patched clients (task 113):
	// each takes the default of the brief when empty.
	"PATH_PROBE_GATE_DOWN":               true,
	"PATH_PROBE_RANK_DOWN":               true,
	"PATH_PROBE_UP":                      true,
	"PATH_PROBE_STALL_MS":                true,
	"PATH_PROBE_DEADLINE_MS":             true,
	"PATH_PROBE_SUSPECT_MS":              true,
	"PATH_PROBE_REPROBE_S":               true,
	"PATH_PROBE_RANK_MIN_INTERVAL_S":     true,
	"PATH_PROBE_MIN_DWELL_S":             true,
	"PATH_PROBE_SWITCH_RATIO":            true,
	"PATH_PROBE_SWITCH_RATIO_NOW":        true,
	"PATH_PROBE_EDGE_PROBE_IF_DIRECT_MS": true,
	"PATH_PROBE_BAD_TTL_S":               true,
	"PATH_PROBE_FAILED_TTL_S":            true,
	"PATH_PROBE_RACE_STAGGER_MS":         true,
	"PATH_PROBE_CONFIRM_S":               true,
	// The built-in inline bots (task 099, ADR 0062): @pic answers with
	// empty lists without SearXNG, @gif has no search by text without the
	// KLIPY key; the import budget is 1 GiB when empty.
	"SEARXNG_URL":               true,
	"KLIPY_API_KEY":             true,
	"INLINE_IMPORT_DAILY_BYTES": true,
	// The bot platform (task 106, ADR 0068): the mini apps and the login of
	// bots are off without the key the bot keys are sealed under, and no
	// mini app opens without the key of the signature of initData.
	"BOT_SECRET_ENCRYPTION_KEY": true,
	"WEBAPP_SIGNING_KEY":        true,
	// The statistics of channels and groups (task 103, ADR 0066): off
	// without the key of the zoom tokens of their graphs.
	"STATS_GRAPH_TOKEN_KEY": true,
	// The HTTPS CDN of large files (task 107, ADR 0071): off without the
	// URL of a delivery, the rest take their defaults when empty.
	"CDN_GCORE_SIGNER":      true,
	"CDN_GCORE_BASE_URL":    true,
	"CDN_GCORE_TOKEN_KEY":   true,
	"CDN_GCORE_MONTHLY_GB":  true,
	"CDN_ROUTES":            true,
	"CDN_ORIGIN_LISTEN":     true,
	"CDN_ORIGIN_AUTH":       true,
	"CDN_URL_TTL":           true,
	"CDN_PREPARE_RATE_MBIT": true,
	"CDN_MIN_FILE_SIZE":     true,
	"CDN_KINDS":             true,
	"CDN_DEV_ALLOW_HTTP":    true,
	"CDN_HEALTH_EVERY":      true,
	"CDN_AVOID_TTL":         true,
	// Delivery 202 of the Russian node (task 109): off without its URL;
	// its monthly budget defaults in defaultVars.
	"CDN_RU_BASE_URL":    true,
	"CDN_RU_TOKEN_KEY":   true,
	"CDN_RU_ORIGIN_AUTH": true,
	"CDN_RU_HEALTH_ADDR": true,
	// The panel's access to the releases service (task 072): off when empty.
	"RELEASES_ADMIN_TOKEN": true,
}

// defaultVars are the variables that take a default when empty, passed as
// ${NAME:-default} with the default given here, the same in every compose
// file that passes them.
var defaultVars = map[string]string{
	// The name of the provider of the login on sites (task 106): the site
	// of Caddy (docker-compose.web.yaml) and the issuer of links.
	"PLUMA_OAUTH_DOMAIN": "oauth.pluma.chat",
	// The monthly budget of delivery 202 (task 109), GB: a bound against a
	// runaway, not the provider's (docs/ops/cdn.md).
	"CDN_RU_MONTHLY_GB": "2000",
}

// passed tells whether val, the compose value of the variable name, gives it
// to the service as it must: ${NAME:?...}, which stops compose when the
// variable is unset or empty, ${NAME:-} for an optional variable, or
// ${NAME:-default} for one of defaultVars.
func passed(name, val string) bool {
	if def, ok := defaultVars[name]; ok {
		return val == "${"+name+":-"+def+"}"
	}
	return strings.HasPrefix(val, "${"+name+":?") || optionalVars[name] && val == "${"+name+":-}"
}

// TestOAuthDomain: the provider of the login on sites answers on the host
// Caddy sends it (task 106): the issuer of links is built from the variable
// that names Caddy's site, and both get it with the same default.
func TestOAuthDomain(t *testing.T) {
	cfg, err := os.ReadFile(filepath.Join(repoRoot, "teamgramd/etc2/links.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var links struct {
		OAuth struct {
			Issuer string `yaml:"Issuer"`
		} `yaml:"OAuth"`
	}
	if err := yaml.Unmarshal(cfg, &links); err != nil {
		t.Fatal(err)
	}
	if want := "https://${PLUMA_OAUTH_DOMAIN}"; links.OAuth.Issuer != want {
		t.Errorf("links.yaml: OAuth.Issuer %q, want %q", links.OAuth.Issuer, want)
	}
	caddy, err := os.ReadFile(filepath.Join(repoRoot, "teamgramd/deploy/web/Caddyfile"))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\{\$PLUMA_OAUTH_DOMAIN\} \{$`).Match(caddy) {
		t.Error("Caddyfile: no site {$PLUMA_OAUTH_DOMAIN}")
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.web.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var web struct {
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &web); err != nil {
		t.Fatal(err)
	}
	if val := web.Services["web"].Environment["PLUMA_OAUTH_DOMAIN"]; !passed("PLUMA_OAUTH_DOMAIN", val) {
		t.Errorf("docker-compose.web.yaml: web gets PLUMA_OAUTH_DOMAIN as %q, not with the default of links", val)
	}
}

// loopbackOnly tells whether every published port is bound to 127.0.0.1.
func loopbackOnly(ports []string) bool {
	for _, p := range ports {
		if !strings.HasPrefix(p, "127.0.0.1:") {
			return false
		}
	}
	return len(ports) > 0
}

// TestTranslateSlots: bff and the translate engine read the slots and the
// context of the engine from the same variables (task 101): bff sends as
// many requests at once as the engine has slots and bounds an answer by
// the context of one, so the two must not differ.
func TestTranslateSlots(t *testing.T) {
	for _, f := range []string{"teamgramd/etc/bff.yaml", "teamgramd/etc2/bff.yaml", "app/bff/bff/etc/bff.yaml"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatal(err)
		}
		var cfg struct {
			Translate map[string]string `yaml:"Translate"`
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if cfg.Translate["Slots"] != "${TRANSLATE_SLOTS}" || cfg.Translate["Context"] != "${TRANSLATE_CTX}" {
			t.Errorf("%s: Translate.Slots %q and Context %q, want ${TRANSLATE_SLOTS} and ${TRANSLATE_CTX}", f, cfg.Translate["Slots"], cfg.Translate["Context"])
		}
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.translate.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var engine struct {
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &engine); err != nil {
		t.Fatal(err)
	}
	env := engine.Services["translate"].Environment
	if !strings.HasPrefix(env["TRANSLATE_SLOTS"], "${TRANSLATE_SLOTS:-") || !strings.HasPrefix(env["TRANSLATE_CTX"], "${TRANSLATE_CTX:-") {
		t.Errorf("docker-compose.translate.yaml: the engine gets slots %q and context %q, not TRANSLATE_SLOTS and TRANSLATE_CTX", env["TRANSLATE_SLOTS"], env["TRANSLATE_CTX"])
	}
}
