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

// Package appreleases keeps the releases of the client apps (task 054,
// ADR 0035): one row of app_releases per signed build CI hands to the
// releases service. The current release of a platform is its latest one
// not withdrawn; the site lists it and help.getAppUpdate offers it.
package appreleases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// The platforms of the clients.
const (
	Android = "android"
	IOS     = "ios"
	MacOS   = "macos"
	Windows = "windows"
	Linux   = "linux"
)

// Platforms are all of them, in the order the site lists them.
var Platforms = []string{Android, IOS, MacOS, Windows, Linux}

// Known tells whether p is a platform.
func Known(p string) bool {
	for _, q := range Platforms {
		if p == q {
			return true
		}
	}
	return false
}

// The kinds of files of a release.
const (
	// KindInstaller is what the site offers for a first install: the APK,
	// the zip or tar.xz of the desktop app.
	KindInstaller = "installer"
	// KindUpdate is a package of the desktop updater (t*upd<version>).
	KindUpdate = "update"
)

// File is a file of a release, stored at <platform>/<version>/<name>.
type File struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	// Arch is the processor of a desktop file (x64, arm64, universal),
	// empty when one file serves all.
	Arch string `json:"arch,omitempty"`
}

// Release is a row of app_releases.
type Release struct {
	ID         int64
	Platform   string
	Version    string // as the client compares it
	Build      int32  // CI's number of the build, rising
	DocumentID int64  // Android: the APK in dfs, offered by help.getAppUpdate
	Files      []File
	NotesEn    string
	NotesRu    string
	CanNotSkip bool
	CreatedAt  int64
}

// Notes are the notes in the language of langCode: Russian for "ru",
// English otherwise; the other language when one is missing.
func (r *Release) Notes(langCode string) string {
	ru := strings.HasPrefix(strings.ToLower(langCode), "ru")
	switch {
	case ru && r.NotesRu != "":
		return r.NotesRu
	case !ru && r.NotesEn != "":
		return r.NotesEn
	case r.NotesEn != "":
		return r.NotesEn
	}
	return r.NotesRu
}

// Installer is the file the site offers for arch ("" for any), nil when
// the release has none.
func (r *Release) Installer(arch string) *File {
	for i := range r.Files {
		if f := &r.Files[i]; f.Kind == KindInstaller && (arch == "" || f.Arch == arch) {
			return f
		}
	}
	return nil
}

// ErrExists: the platform has a release of this build already.
var ErrExists = errors.New("appreleases: the build is released already")

const columns = "id, platform, version, build, document_id, files, notes_en, notes_ru, can_not_skip, created_at"

type row struct {
	ID         int64  `db:"id"`
	Platform   string `db:"platform"`
	Version    string `db:"version"`
	Build      int32  `db:"build"`
	DocumentID int64  `db:"document_id"`
	Files      string `db:"files"`
	NotesEn    string `db:"notes_en"`
	NotesRu    string `db:"notes_ru"`
	CanNotSkip bool   `db:"can_not_skip"`
	CreatedAt  int64  `db:"created_at"`
}

func (r *row) release() (*Release, error) {
	out := &Release{ID: r.ID, Platform: r.Platform, Version: r.Version, Build: r.Build, DocumentID: r.DocumentID,
		NotesEn: r.NotesEn, NotesRu: r.NotesRu, CanNotSkip: r.CanNotSkip, CreatedAt: r.CreatedAt}
	if err := json.Unmarshal([]byte(r.Files), &out.Files); err != nil {
		return nil, fmt.Errorf("appreleases: files of release %d: %w", r.ID, err)
	}
	return out, nil
}

// Insert adds a release; ErrExists for a build the platform has.
func Insert(ctx context.Context, db *sqlx.DB, r *Release) error {
	files, err := json.Marshal(r.Files)
	if err != nil {
		return err
	}
	res, err := db.Exec(ctx,
		"INSERT INTO app_releases (platform, version, build, document_id, files, notes_en, notes_ru, can_not_skip, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		r.Platform, r.Version, r.Build, r.DocumentID, string(files), r.NotesEn, r.NotesRu, r.CanNotSkip, r.CreatedAt)
	if sqlx.IsDuplicate(err) {
		return ErrExists
	}
	if err != nil {
		return err
	}
	r.ID, err = res.LastInsertId()
	return err
}

// Current is the platform's latest release not withdrawn, nil for none.
func Current(ctx context.Context, db *sqlx.DB, platform string) (*Release, error) {
	var rows []row
	if err := db.QueryRowsPartial(ctx, &rows,
		"SELECT "+columns+" FROM app_releases WHERE platform = ? AND withdrawn = 0 ORDER BY build DESC LIMIT 1", platform); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].release()
}

// ByBuild is the platform's release of build, nil for none.
func ByBuild(ctx context.Context, db *sqlx.DB, platform string, build int32) (*Release, error) {
	var rows []row
	if err := db.QueryRowsPartial(ctx, &rows,
		"SELECT "+columns+" FROM app_releases WHERE platform = ? AND build = ?", platform, build); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].release()
}

// SetWithdrawn withdraws a release, or brings it back: the current release
// goes back to the one before, or forward again. It tells whether the
// platform had the build.
func SetWithdrawn(ctx context.Context, db *sqlx.DB, platform string, build int32, withdrawn bool) (bool, error) {
	res, err := db.Exec(ctx, "UPDATE app_releases SET withdrawn = ? WHERE platform = ? AND build = ?", withdrawn, platform, build)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	// No change: the row may be in that state already.
	r, err := ByBuild(ctx, db, platform, build)
	return r != nil, err
}

// androidVersion is versionName of a release of Pluma for Android: the
// version of Telegram it is built from and Pluma's build, digits only
// (task 054: Android parses each part with Integer.parseInt,
// SharedConfig.java:811-824 at aac1efcb).
var androidVersion = regexp.MustCompile(`^\d{1,4}(\.\d{1,9}){3}$`)

// iosVersion is CFBundleShortVersionString of the iOS app: the version of
// Telegram it is built from; Pluma's build goes apart (Release.Build).
var iosVersion = regexp.MustCompile(`^\d{1,4}(\.\d{1,4}){1,2}$`)

// ValidVersion checks the version of a release of the platform: Android
// 1.2.3.4; iOS 1.2.3; desktop the integer AppVersion of tdesktop.
func ValidVersion(platform, version string) bool {
	switch platform {
	case Android:
		return androidVersion.MatchString(version)
	case IOS:
		return iosVersion.MatchString(version)
	case MacOS, Windows, Linux:
		n, err := strconv.ParseUint(version, 10, 32)
		return err == nil && n > 0
	}
	return false
}

// AndroidInstalled is versionName in the app_version Android sends,
// "<versionName> (<versionCode>)" with " beta" or " pbeta" after it
// (ConnectionsManager.java:223-228 at aac1efcb); false for another form.
func AndroidInstalled(appVersion string) (string, bool) {
	name, _, ok := strings.Cut(appVersion, " (")
	if !ok || name == "" {
		return "", false
	}
	for _, p := range strings.Split(name, ".") {
		if _, err := strconv.Atoi(p); err != nil {
			return "", false
		}
	}
	return name, true
}

// AndroidNewer tells whether Android takes offered as newer than
// installed: its versionBiggerOrEqual(installed, offered) is false
// (SharedConfig.java:801, 811-824 at aac1efcb). It compares the parts both
// have, so 12.9.1 takes neither 12.9.1.5 nor 12.9.1 as newer. Versions
// with a part that is not a number are never newer: Android would throw
// on them.
func AndroidNewer(installed, offered string) bool {
	a, b := strings.Split(installed, "."), strings.Split(offered, ".")
	for i := 0; i < min(len(a), len(b)); i++ {
		x, err1 := strconv.Atoi(a[i])
		y, err2 := strconv.Atoi(b[i])
		switch {
		case err1 != nil || err2 != nil:
			return false
		case x < y:
			return true
		case x > y:
			return false
		}
	}
	return false
}

// IOSInstalled reads the app_version the iOS app of Pluma sends:
// "<version> (<build>)", with " adhoc" after it from a build installed
// from the site's install service rather than TestFlight (the patch of
// task 054: TestFlight updates its own builds, ad hoc ones are offered
// the install page). false for another form, Telegram's own included
// (only the version, AppDelegate.swift:528 at 6ad963e5).
func IOSInstalled(appVersion string) (build int32, adhoc, ok bool) {
	rest := appVersion
	if strings.HasSuffix(rest, " adhoc") {
		rest, adhoc = strings.TrimSuffix(rest, " adhoc"), true
	}
	_, num, found := strings.Cut(rest, " (")
	if !found || !strings.HasSuffix(num, ")") {
		return 0, false, false
	}
	n, err := strconv.ParseInt(strings.TrimSuffix(num, ")"), 10, 32)
	if err != nil || n <= 0 {
		return 0, false, false
	}
	return int32(n), adhoc, true
}

// updatePackage is the name of a package of tdesktop's updater: the
// prefix Packer gives it for its system and processor, then the version
// (packer.cpp:498-502 at a1e89e1f). The updater finds a downloaded package
// again only by these names (FindUpdateFile, update_checker.cpp:292-315).
var updatePackage = regexp.MustCompile(`^(tupdate|tx64upd|tarm64upd|tmacupd|tarmacupd|tlinuxupd)(\d+)$`)

// updaterKeys are the systems each package prefix serves, by the key the
// updater asks with (Platform::AutoUpdateKey of lib_base 82d182a2:
// base_info_win.cpp:322-330, base_info_mac.mm:217-224,
// base_info_linux.cpp:212-214). The macOS app of Pluma is universal: one
// tmacupd package serves Intel and Apple processors, unless the release
// has a tarmacupd of its own.
var updaterKeys = map[string]struct {
	platform string
	keys     []string
}{
	"tupdate":   {Windows, []string{"win"}},
	"tx64upd":   {Windows, []string{"win64"}},
	"tarm64upd": {Windows, []string{"winarm"}},
	"tmacupd":   {MacOS, []string{"mac", "armac"}},
	"tarmacupd": {MacOS, []string{"armac"}},
	"tlinuxupd": {Linux, []string{"linux"}},
}

// UpdaterKeys are the keys of the updater a desktop update package of the
// release serves; false when name is not a package of the release's
// platform and version.
func UpdaterKeys(platform, version, name string) ([]string, bool) {
	m := updatePackage.FindStringSubmatch(name)
	if m == nil || m[2] != version {
		return nil, false
	}
	k := updaterKeys[m[1]]
	if k.platform != platform {
		return nil, false
	}
	return k.keys, true
}

// UpdaterLink is the update package a key of the updater is served.
type UpdaterLink struct {
	Release *Release
	Name    string // the file within the release
}

// UpdaterLinks are the update packages of the desktop releases by the key
// of the updater. A package made for one key wins over one that serves
// several.
func UpdaterLinks(releases []*Release) map[string]UpdaterLink {
	out := map[string]UpdaterLink{}
	specific := map[string]bool{}
	for _, r := range releases {
		for _, f := range r.Files {
			if f.Kind != KindUpdate {
				continue
			}
			keys, ok := UpdaterKeys(r.Platform, r.Version, f.Name)
			if !ok {
				continue
			}
			for _, k := range keys {
				if specific[k] && len(keys) > 1 {
					continue
				}
				out[k] = UpdaterLink{r, f.Name}
				specific[k] = len(keys) == 1
			}
		}
	}
	return out
}
