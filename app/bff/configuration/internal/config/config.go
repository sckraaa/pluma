// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package config

import (
	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/teamgram-server/pkg/iosupdate"
	"github.com/teamgram/teamgram-server/pkg/staticmap"
	"github.com/teamgram/teamgram-server/pkg/translate"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	Mysql             sqlx.Config // dismissed suggestions
	UserClient        zrpc.RpcClientConf
	AuthsessionClient zrpc.RpcClientConf // the client's time zone, its app (help.getAppUpdate)
	// MediaClient gives the APK of an Android release (help.getAppUpdate).
	MediaClient zrpc.RpcClientConf
	// ReleasesURL is where the releases are, the site's /dl/ (task 054):
	// the install page of the iPhone app, and the update prefix of the
	// desktop app (help.getConfig) unless ReleasesFilesURL names another.
	ReleasesURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ReleasesRoutes is PLUMA_RELEASES_ROUTES, the mirrors of that site for
	// the clients of some countries (task 113, docs/ops/releases.md):
	// "RU,BY=https://pluma.sckra.dev"; the clients of every other country
	// get ReleasesURL.
	ReleasesRoutes string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ReleasesFilesURL is PLUMA_RELEASES_FILES_URL, the CDN the clients
	// download the files of the site's /dl/ from (task 115,
	// docs/ops/releases.md): the update prefix of the desktop app; empty:
	// the site itself.
	ReleasesFilesURL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ReleasesFilesRoutes is PLUMA_RELEASES_FILES_ROUTES, the caches of
	// /dl/ for the clients of some countries: "RU,BY=https://r1.sckra.dev";
	// the clients of every other country get ReleasesFilesURL.
	ReleasesFilesRoutes string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// IOSUpdate lets an Ad Hoc build of the iPhone app update from the app
	// (task 085); off without its secret.
	IOSUpdate iosupdate.Settings `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Translate is the translation engine of bff (task 101, ADR 0064);
	// without its URL the app config hides the translation from the
	// clients.
	Translate translate.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// GifSearch is whether @gif searches GIFs by text (KLIPY, task 099,
	// ADR 0062): the app config then offers the emoji categories of the GIF
	// tab, which it leaves empty otherwise.
	GifSearch bool `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Maps is the Maps block of bff (task 112, ADR 0070): with its
	// PublicURL the app config hands the clients the public map tiles of
	// their interactive maps and the styles there.
	Maps staticmap.Config `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Edge is the edges of the patched clients, pluma_edge of the app
	// config (task 109, ADR 0072, docs/ops/edge.md): no edges without
	// WSURLs.
	Edge EdgeConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Probe is the settings of the path probes of the patched clients,
	// pluma_probe of the app config (task 113, ADR 0073 §7,
	// docs/ops/edge.md): an empty value takes the default of the brief.
	Probe ProbeConfig `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// EdgeConfig is the Edge block of bff.yaml, parsed by appconfig.ParseEdges
// at the start.
type EdgeConfig struct {
	// WSURLs is EDGE_WS_URLS, the edges by name:
	// "gcore=wss://<prefix>.gcorecdn.com/apiws,ru=wss://ru.sckra.dev/apiws".
	WSURLs string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Routes is EDGE_ROUTES, the edges and their order by the country of
	// the client: "RU:edge,direct:ru,gcore;*:direct,edge:gcore,ru".
	Routes string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// StickyS is EDGE_STICKY_S, how many seconds a client keeps to an edge
	// it switched to ("1800").
	StickyS string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// ProbeConfig is the Probe block of bff.yaml, the PATH_PROBE_* variables,
// parsed by appconfig.ParseProbe at the start.
type ProbeConfig struct {
	GateDown            string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	RankDown            string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	Up                  string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	StallMs             string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	DeadlineMs          string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	SuspectMs           string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	ReprobeS            string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	RankMinIntervalS    string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	MinDwellS           string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	SwitchRatio         string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	SwitchRatioNow      string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	EdgeProbeIfDirectMs string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	BadTTLS             string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	FailedTTLS          string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	RaceStaggerMs       string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	ConfirmS            string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Variables is the block by the names of its variables, as
// appconfig.ParseProbe takes them.
func (p ProbeConfig) Variables() map[string]string {
	return map[string]string{
		"PATH_PROBE_GATE_DOWN":               p.GateDown,
		"PATH_PROBE_RANK_DOWN":               p.RankDown,
		"PATH_PROBE_UP":                      p.Up,
		"PATH_PROBE_STALL_MS":                p.StallMs,
		"PATH_PROBE_DEADLINE_MS":             p.DeadlineMs,
		"PATH_PROBE_SUSPECT_MS":              p.SuspectMs,
		"PATH_PROBE_REPROBE_S":               p.ReprobeS,
		"PATH_PROBE_RANK_MIN_INTERVAL_S":     p.RankMinIntervalS,
		"PATH_PROBE_MIN_DWELL_S":             p.MinDwellS,
		"PATH_PROBE_SWITCH_RATIO":            p.SwitchRatio,
		"PATH_PROBE_SWITCH_RATIO_NOW":        p.SwitchRatioNow,
		"PATH_PROBE_EDGE_PROBE_IF_DIRECT_MS": p.EdgeProbeIfDirectMs,
		"PATH_PROBE_BAD_TTL_S":               p.BadTTLS,
		"PATH_PROBE_FAILED_TTL_S":            p.FailedTTLS,
		"PATH_PROBE_RACE_STAGGER_MS":         p.RaceStaggerMs,
		"PATH_PROBE_CONFIRM_S":               p.ConfirmS,
	}
}
