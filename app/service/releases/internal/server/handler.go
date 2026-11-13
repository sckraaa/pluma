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

package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/appreleases"

	"github.com/zeromicro/go-zero/core/logx"
)

// The releases service (task 054, ADR 0035), behind Caddy at /dl/ of the
// site:
//
//   - POST /dl/upload: CI hands over a signed build with the upload token;
//     the files are written to DataDir/files/<platform>/<version>/, the
//     APK of Android is stored as a document in dfs, and the release is a
//     row of app_releases. The same build again is 409; for a release
//     with files, the version of another build is 422 (ADR 0037).
//   - GET /dl/releases.json: the current release of each platform, which
//     the site reads to offer the downloads.
//   - GET /dl/<platform>/latest[/<arch>]: a redirect to the current
//     installer, the address the site's buttons keep.
//   - GET /dl/<platform>/<version>/<name>: a file; it never changes.
//   - GET /dl/desktop/current4: the update packages of the desktop app for
//     its updater, which asks <autoupdate_url_prefix>/current4 (ADR 0035);
//     GET /dl/desktop/<platform>/<version>/<name> is a package.
//
// The files are written once, fully and synced, before the row that makes
// them the current release: a client never sees a release whose files are
// not all there.

// maxUpload bounds one upload: the desktop builds of all architectures of
// one platform with their update packages.
const maxUpload = 2 << 30

// androidMime is the type Android opens an APK by
// (AndroidUtilities.openForView at aac1efcb, by the file name's extension).
const androidMime = "application/vnd.android.package-archive"

// Documents stores the APK of Android as a document (importer.RPCDocuments).
type Documents interface {
	Import(ctx context.Context, doc *mtproto.Document, file []byte, thumbs map[string][]byte, id int64) (*mtproto.Document, error)
}

type Handler struct {
	PublicURL string
	DataDir   string
	Token     string
	// AdminToken guards the panel's routes (/internal/releases); empty
	// turns them off (503).
	AdminToken string
	DB         *sqlx.DB
	Documents  Documents
	Now        func() time.Time

	publishing sync.Mutex
}

var (
	fileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	archName = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
)

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /dl/upload", h.upload)
	mux.HandleFunc("GET /dl/releases.json", h.releasesJSON)
	mux.HandleFunc("GET /dl/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /dl/{platform}/latest", h.latest)
	mux.HandleFunc("GET /dl/{platform}/latest/{arch}", h.latest)
	mux.HandleFunc("GET /dl/{platform}/{version}/{name}", h.file)
	mux.HandleFunc("GET /dl/desktop/current4", h.desktopCurrent)
	mux.HandleFunc("GET /dl/desktop/{platform}/{version}/{name}", h.file)
	// The panel's routes: not under /dl/, Caddy does not proxy them.
	mux.HandleFunc("GET /internal/releases", h.adminList)
	mux.HandleFunc("POST /internal/releases/{platform}/{build}/withdraw", h.adminWithdraw)
	mux.HandleFunc("POST /internal/releases/{platform}/{build}/restore", h.adminRestore)
	return mux
}

// upload takes a release: the form fields platform, version, build,
// notes_en, notes_ru, can_not_skip ("1" makes it mandatory, see
// canNotSkip) and the files, each a part named installer or update, with
// ".<arch>" for a desktop file of one processor (installer.arm64), and its
// file name.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(auth), []byte(h.Token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "multipart form expected", http.StatusBadRequest)
		return
	}
	work, err := os.MkdirTemp(filepath.Join(h.DataDir, "work"), "upload-")
	if err != nil {
		h.internal(w, err)
		return
	}
	defer os.RemoveAll(work)

	rel := &appreleases.Release{CreatedAt: h.Now().Unix()}
	fields := map[string]string{}
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
			return
		}
		kind, arch, _ := strings.Cut(p.FormName(), ".")
		if kind != appreleases.KindInstaller && kind != appreleases.KindUpdate {
			v, err := io.ReadAll(io.LimitReader(p, 64<<10))
			if err != nil {
				http.Error(w, "bad form: "+err.Error(), http.StatusBadRequest)
				return
			}
			fields[p.FormName()] = strings.TrimSpace(string(v))
			continue
		}
		name := p.FileName()
		if !fileName.MatchString(name) || arch != "" && !archName.MatchString(arch) {
			http.Error(w, fmt.Sprintf("bad file %q", name), http.StatusBadRequest)
			return
		}
		f, err := saveFile(filepath.Join(work, name), p)
		if err != nil {
			http.Error(w, "upload: "+err.Error(), http.StatusBadRequest)
			return
		}
		f.Name, f.Kind, f.Arch = name, kind, arch
		rel.Files = append(rel.Files, f)
	}

	rel.Platform, rel.Version = fields["platform"], fields["version"]
	build, err := strconv.ParseInt(fields["build"], 10, 32)
	switch {
	case !appreleases.Known(rel.Platform):
		http.Error(w, "unknown platform", http.StatusBadRequest)
		return
	case !appreleases.ValidVersion(rel.Platform, rel.Version):
		http.Error(w, "bad version for "+rel.Platform, http.StatusBadRequest)
		return
	case err != nil || build <= 0:
		http.Error(w, "bad build number", http.StatusBadRequest)
		return
	}
	rel.Build = int32(build)
	rel.NotesEn, rel.NotesRu = fields["notes_en"], fields["notes_ru"]
	if rel.CanNotSkip, err = canNotSkip(rel.Platform, fields["can_not_skip"]); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err = checkFiles(rel); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// One release is published at a time: the check that it may be
	// published (released), the APK's document, its files' directory and
	// its row go together, so that an upload refused here leaves no
	// document behind in dfs.
	h.publishing.Lock()
	defer h.publishing.Unlock()
	if status, msg, err := h.released(r.Context(), rel); err != nil {
		h.internal(w, err)
		return
	} else if status != 0 {
		http.Error(w, msg, status)
		return
	}
	if rel.Platform == appreleases.Android {
		if rel.DocumentID, err = h.importAPK(r.Context(), rel, filepath.Join(work, rel.Files[0].Name)); err != nil {
			h.internal(w, err)
			return
		}
	}
	dir, err := h.publishFiles(rel, work)
	if err != nil {
		h.removeDir(dir)
		h.internal(w, err)
		return
	}
	if err = appreleases.Insert(r.Context(), h.DB, rel); err != nil {
		// Not released: its files go, so that CI may upload it again.
		h.removeDir(dir)
		if errors.Is(err, appreleases.ErrExists) {
			http.Error(w, "the build is released already", http.StatusConflict)
			return
		}
		h.internal(w, err)
		return
	}
	logx.WithContext(r.Context()).Infof("releases: %s %s (build %d) released, %d files", rel.Platform, rel.Version, rel.Build, len(rel.Files))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.view(rel))
}

// canNotSkip reads the can_not_skip field of an upload: "1" makes the
// release mandatory from its first offer, "0" or none leaves it optional.
// Android keeps the offer of a version it was given and does not look at
// the answer for that version again (LaunchActivity.java:6153-6155,
// DrKLO/Telegram 7be672ea), so a release made mandatory only after its
// upload may stay optional for the apps that checked in between; CI sets
// the flag here instead (task 095). Any other value is refused rather than
// taken for optional, and so is "1" for the desktop platforms, whose
// updater has no mandatory updates.
func canNotSkip(platform, v string) (bool, error) {
	switch v {
	case "", "0":
		return false, nil
	case "1":
		if !appreleases.CanBeMandatory(platform) {
			return false, errors.New(platform + ": the desktop app has no mandatory updates; only android and ios do")
		}
		return true, nil
	}
	return false, fmt.Errorf("bad can_not_skip %q: 1 or 0", v)
}

// checkFiles checks the files of a release: Android one APK; iOS none (the
// IPA goes to the install service); desktop at least one installer, one
// installer and one update package per processor.
func checkFiles(rel *appreleases.Release) error {
	seen := map[string]bool{}
	for _, f := range rel.Files {
		if seen[f.Name] {
			return fmt.Errorf("file %s twice", f.Name)
		}
		seen[f.Name] = true
		key := f.Kind + "." + f.Arch
		if seen[key] {
			return fmt.Errorf("two %s files for %q", f.Kind, f.Arch)
		}
		seen[key] = true
	}
	switch rel.Platform {
	case appreleases.Android:
		if len(rel.Files) != 1 || rel.Files[0].Kind != appreleases.KindInstaller || !strings.HasSuffix(rel.Files[0].Name, ".apk") {
			return errors.New("android: one installer, an .apk")
		}
	case appreleases.IOS:
		if len(rel.Files) != 0 {
			return errors.New("ios: no files; the IPA goes to the install service")
		}
	default:
		if rel.Installer("") == nil {
			return errors.New(rel.Platform + ": no installer")
		}
		for _, f := range rel.Files {
			if _, ok := appreleases.UpdaterKeys(rel.Platform, rel.Build, f.Name); f.Kind == appreleases.KindUpdate && !ok {
				return fmt.Errorf("%s: %s is not an update package of %s build %d", rel.Platform, f.Name, rel.Platform, rel.Build)
			}
		}
	}
	return nil
}

// saveFile writes a part to path, synced, and returns its size and hash.
func saveFile(path string, src io.Reader) (appreleases.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640) //nolint:gosec // G304: a name checked against fileName in the service's work directory
	if err != nil {
		return appreleases.File{}, err
	}
	defer f.Close()
	sum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, sum), src)
	if err != nil {
		return appreleases.File{}, err
	}
	if n == 0 {
		return appreleases.File{}, errors.New("empty file")
	}
	if err = f.Sync(); err != nil {
		return appreleases.File{}, err
	}
	return appreleases.File{Size: n, SHA256: hex.EncodeToString(sum.Sum(nil))}, f.Close()
}

// importAPK stores the APK in dfs as a document Android takes for an update:
// named Pluma-<version>.apk, of the APK's type (help.appUpdate.document,
// UpdateAppAlertDialog of the standalone app at aac1efcb).
func (h *Handler) importAPK(ctx context.Context, rel *appreleases.Release, path string) (int64, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the file this upload wrote
	if err != nil {
		return 0, err
	}
	doc := mtproto.MakeTLDocument(&mtproto.Document{
		MimeType: androidMime,
		Date:     int32(h.Now().Unix()),
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeFilename(&mtproto.DocumentAttribute{FileName: "Pluma-" + rel.Version + ".apk"}).To_DocumentAttribute(),
		},
	}).To_Document()
	stored, err := h.Documents.Import(ctx, doc, data, nil, 0)
	if err != nil {
		return 0, fmt.Errorf("releases: store the APK: %w", err)
	}
	return stored.GetId(), nil
}

// released tells whether rel may be published, 0 when it may.
//   - 409: the platform has its build already, of the same version and with
//     the same files. That is CI's repeat when a job runs again, and the
//     scripts of CI take 409 for one. The release keeps its can_not_skip:
//     an optional repeat does not undo pluma-admin release mandatory.
//   - 422: that repeat asks for a mandatory release, and the build is
//     released optional. Taking it for a repeat would leave the release
//     optional while CI reports it done; the operator makes it mandatory
//     with pluma-admin, knowing that the Android apps that were offered it
//     already keep it optional (canNotSkip).
//   - 422: the platform has its build already with another version or other
//     files (a run number that started again, a version changed under the
//     same number): no repeat, and the scripts must fail on it. And a
//     release with files that has the version of another build. Its
//     version names its files' directory (files/<platform>/<version>),
//     which never changes; the scripts must fail on it, not take it for a
//     repeat.
//
// A release without files, an iOS one, may repeat a version: it is
// Telegram's version, the same across Pluma's builds, and help.getAppUpdate
// compares the build.
func (h *Handler) released(ctx context.Context, rel *appreleases.Release) (int, string, error) {
	if old, err := appreleases.ByBuild(ctx, h.DB, rel.Platform, rel.Build); err != nil {
		return 0, "", err
	} else if old != nil {
		if sameRelease(old, rel) {
			if rel.CanNotSkip && !old.CanNotSkip {
				return http.StatusUnprocessableEntity, fmt.Sprintf("build %d is released already, optional; pluma-admin release mandatory makes it mandatory", old.Build), nil
			}
			return http.StatusConflict, "the build is released already", nil
		}
		return http.StatusUnprocessableEntity, fmt.Sprintf("build %d is released already, as version %s with other files", old.Build, old.Version), nil
	}
	if len(rel.Files) == 0 {
		return 0, "", nil
	}
	if old, err := appreleases.ByVersion(ctx, h.DB, rel.Platform, rel.Version); err != nil {
		return 0, "", err
	} else if old != nil {
		return http.StatusUnprocessableEntity, fmt.Sprintf("version %s is released already, as build %d", rel.Version, old.Build), nil
	}
	return 0, "", nil
}

// sameRelease tells whether rel repeats old: the same version and the same
// files, by name, kind, processor and SHA-256. The notes may differ.
func sameRelease(old, rel *appreleases.Release) bool {
	if old.Version != rel.Version || len(old.Files) != len(rel.Files) {
		return false
	}
	files := make(map[appreleases.File]bool, len(old.Files))
	for _, f := range old.Files {
		files[appreleases.File{Name: f.Name, Kind: f.Kind, Arch: f.Arch, SHA256: f.SHA256}] = true
	}
	for _, f := range rel.Files {
		if !files[appreleases.File{Name: f.Name, Kind: f.Kind, Arch: f.Arch, SHA256: f.SHA256}] {
			return false
		}
	}
	return true
}

// publishFiles moves the files of a release to their directory, named by
// its version, and syncs the directories, so that the row inserted next
// never outlives the files in a crash. The caller holds publishing and has
// found the version unreleased: a directory there is what an upload
// interrupted before its row left, and goes. It returns the directory, ""
// for a release without files.
func (h *Handler) publishFiles(rel *appreleases.Release, work string) (string, error) {
	if len(rel.Files) == 0 {
		return "", nil
	}
	parent := filepath.Join(h.DataDir, "files", rel.Platform)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return "", err
	}
	dir := filepath.Join(parent, rel.Version)
	if _, err := os.Stat(dir); err == nil {
		logx.Infof("releases: %s has no release row, an interrupted upload's; removed", dir)
		if err = os.RemoveAll(dir); err != nil {
			return "", err
		}
	}
	if err := syncDir(work); err != nil {
		return "", err
	}
	if err := os.Rename(work, dir); err != nil {
		return "", err
	}
	if err := syncDir(parent); err != nil {
		return dir, err
	}
	// The work directory is gone: recreate it for the deferred clean-up.
	return dir, os.MkdirAll(work, 0o750)
}

// syncDir makes the entries of directory d durable.
func syncDir(d string) error {
	f, err := os.Open(d)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// removeDir removes the files of a release that was not released; "" is
// none.
func (h *Handler) removeDir(dir string) {
	if dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		logx.Errorf("releases: remove %s: %v", dir, err)
	}
}

func (h *Handler) internal(w http.ResponseWriter, err error) {
	logx.Errorf("releases: %v", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// fileURL is the address of a file of a release.
func (h *Handler) fileURL(rel *appreleases.Release, f *appreleases.File) string {
	return h.PublicURL + "/dl/" + rel.Platform + "/" + rel.Version + "/" + f.Name
}

// releaseView is a release as releases.json and the upload answer show it.
type releaseView struct {
	Version    string     `json:"version"`
	Build      int32      `json:"build"`
	Date       int64      `json:"date"`
	NotesEn    string     `json:"notes_en,omitempty"`
	NotesRu    string     `json:"notes_ru,omitempty"`
	URL        string     `json:"url"`
	Size       int64      `json:"size,omitempty"`
	SHA256     string     `json:"sha256,omitempty"`
	Installers []fileView `json:"installers,omitempty"`
}

type fileView struct {
	Arch   string `json:"arch,omitempty"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (h *Handler) view(rel *appreleases.Release) releaseView {
	v := releaseView{Version: rel.Version, Build: rel.Build, Date: rel.CreatedAt, NotesEn: rel.NotesEn, NotesRu: rel.NotesRu}
	if rel.Platform == appreleases.IOS {
		// The install page of the site (ad hoc, iosinstall).
		v.URL = h.PublicURL + "/iphone/install"
		return v
	}
	v.URL = h.PublicURL + "/dl/" + rel.Platform + "/latest"
	for i := range rel.Files {
		f := &rel.Files[i]
		if f.Kind != appreleases.KindInstaller {
			continue
		}
		v.Installers = append(v.Installers, fileView{Arch: f.Arch, URL: h.fileURL(rel, f), Size: f.Size, SHA256: f.SHA256})
	}
	if len(v.Installers) == 1 {
		v.Size, v.SHA256 = v.Installers[0].Size, v.Installers[0].SHA256
	}
	return v
}

// releasesJSON is the current release of each platform that has one.
func (h *Handler) releasesJSON(w http.ResponseWriter, r *http.Request) {
	out := map[string]releaseView{}
	for _, p := range appreleases.Platforms {
		rel, err := appreleases.Current(r.Context(), h.DB, p)
		if err != nil {
			h.internal(w, err)
			return
		}
		if rel != nil {
			out[p] = h.view(rel)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(map[string]any{"platforms": out})
}

// desktopCurrent answers the updater of tdesktop: for each key of its
// systems, the version of the current package and its link, which the
// updater adds to its prefix (HttpChecker::parseResponse and
// ParseCommonMap, update_checker.cpp:606-680, :804-850 at a1e89e1f). It
// takes a version above its own: Pluma's patch makes that the build number
// (PlumaBuild), the release's build, which also names the package. The
// suffix 4 is the updater
// of lib_base 82d182a2 on every system the app runs on (AutoUpdateVersion:
// 2 only below macOS 10.13 or glibc 2.28, which Pluma does not build for);
// another suffix is another updater and gets nothing.
func (h *Handler) desktopCurrent(w http.ResponseWriter, r *http.Request) {
	var current []*appreleases.Release
	for _, p := range []string{appreleases.MacOS, appreleases.Windows, appreleases.Linux} {
		rel, err := appreleases.Current(r.Context(), h.DB, p)
		if err != nil {
			h.internal(w, err)
			return
		}
		if rel != nil {
			current = append(current, rel)
		}
	}
	type channel struct {
		Released string `json:"released"`
		Link     string `json:"link"`
	}
	out := map[string]map[string]channel{}
	for key, l := range appreleases.UpdaterLinks(current) {
		out[key] = map[string]channel{"stable": {
			Released: strconv.Itoa(int(l.Release.Build)),
			Link:     "/" + l.Release.Platform + "/" + l.Release.Version + "/" + l.Name,
		}}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(out)
}

// latest redirects to the current installer of the platform (for arch).
func (h *Handler) latest(w http.ResponseWriter, r *http.Request) {
	platform, arch := r.PathValue("platform"), r.PathValue("arch")
	if !appreleases.Known(platform) || platform == appreleases.IOS {
		http.NotFound(w, r)
		return
	}
	rel, err := appreleases.Current(r.Context(), h.DB, platform)
	if err != nil {
		h.internal(w, err)
		return
	}
	var f *appreleases.File
	if rel != nil {
		f = rel.Installer(arch)
	}
	if f == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, h.fileURL(rel, f), http.StatusFound) //nolint:gosec // the site's own address and a stored file name, not the request's
}

// file serves a file of a release; a released file never changes.
func (h *Handler) file(w http.ResponseWriter, r *http.Request) {
	platform, version, name := r.PathValue("platform"), r.PathValue("version"), r.PathValue("name")
	if !appreleases.Known(platform) || !appreleases.ValidVersion(platform, version) || !fileName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(h.DataDir, "files", platform, version, name)
	f, err := os.Open(path) //nolint:gosec // G304: segments checked above
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	if strings.HasSuffix(name, ".apk") {
		w.Header().Set("Content-Type", androidMime)
	}
	http.ServeContent(&progressWriter{ResponseWriter: w, rc: http.NewResponseController(w)}, r, name, st.ModTime(), f)
}

// downloadStall is how long a download may stop before its connection is
// closed. A download has no deadline of its own: a slow one that moves goes
// on (an installer of a few hundred MB on a slow line takes long).
const downloadStall = 2 * time.Minute

// progressWriter moves the write deadline on with every write.
type progressWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func (p *progressWriter) Write(b []byte) (int, error) {
	if err := p.rc.SetWriteDeadline(time.Now().Add(downloadStall)); err != nil {
		return 0, err
	}
	return p.ResponseWriter.Write(b)
}

// adminAuth allows a panel's request: the admin token, a constant-time
// compare. 503 when the routes are off, 403 for a wrong token.
func (h *Handler) adminAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.AdminToken == "" {
		http.Error(w, "not configured", http.StatusServiceUnavailable)
		return false
	}
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(auth), []byte(h.AdminToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

// adminRelease is a build as the panel lists it.
type adminRelease struct {
	Version    string             `json:"version"`
	Build      int32              `json:"build"`
	NotesRu    string             `json:"notes_ru"`
	NotesEn    string             `json:"notes_en"`
	CanNotSkip bool               `json:"can_not_skip"`
	CreatedAt  int64              `json:"created_at"`
	Withdrawn  bool               `json:"withdrawn"`
	Files      []appreleases.File `json:"files"`
}

// adminPlatform is a platform's builds and its current one.
type adminPlatform struct {
	Platform string         `json:"platform"`
	Current  int32          `json:"current"`
	Builds   []adminRelease `json:"builds"`
}

func (h *Handler) adminState(r *http.Request, platform string) (*adminPlatform, error) {
	list, err := appreleases.List(r.Context(), h.DB, platform, 30)
	if err != nil {
		return nil, err
	}
	cur, err := appreleases.Current(r.Context(), h.DB, platform)
	if err != nil {
		return nil, err
	}
	out := &adminPlatform{Platform: platform, Builds: make([]adminRelease, len(list))}
	if cur != nil {
		out.Current = cur.Build
	}
	for i, l := range list {
		out.Builds[i] = adminRelease{Version: l.Version, Build: l.Build, NotesRu: l.NotesRu, NotesEn: l.NotesEn,
			CanNotSkip: l.CanNotSkip, CreatedAt: l.CreatedAt, Withdrawn: l.Withdrawn, Files: l.Files}
	}
	return out, nil
}

func (h *Handler) adminList(w http.ResponseWriter, r *http.Request) {
	if !h.adminAuth(w, r) {
		return
	}
	out := struct {
		Platforms []*adminPlatform `json:"platforms"`
	}{}
	for _, p := range appreleases.Platforms {
		state, err := h.adminState(r, p)
		if err != nil {
			h.internal(w, err)
			return
		}
		out.Platforms = append(out.Platforms, state)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		logx.Errorf("releases: encode: %v", err)
	}
}

func (h *Handler) adminWithdraw(w http.ResponseWriter, r *http.Request) { h.adminSet(w, r, true) }

func (h *Handler) adminRestore(w http.ResponseWriter, r *http.Request) { h.adminSet(w, r, false) }

// adminSet rolls a platform back (withdraws the build) or brings a withdrawn
// build back; the one publish lock keeps it from interleaving with an upload.
func (h *Handler) adminSet(w http.ResponseWriter, r *http.Request, withdrawn bool) {
	if !h.adminAuth(w, r) {
		return
	}
	platform := r.PathValue("platform")
	build, err := strconv.ParseInt(r.PathValue("build"), 10, 32)
	if !appreleases.Known(platform) || err != nil || build <= 0 {
		http.Error(w, "bad platform or build", http.StatusBadRequest)
		return
	}
	h.publishing.Lock()
	defer h.publishing.Unlock()
	found, err := appreleases.SetWithdrawn(r.Context(), h.DB, platform, int32(build), withdrawn)
	if err != nil {
		h.internal(w, err)
		return
	}
	if !found {
		http.Error(w, "no such build", http.StatusNotFound)
		return
	}
	state, err := h.adminState(r, platform)
	if err != nil {
		h.internal(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(state); err != nil {
		logx.Errorf("releases: encode: %v", err)
	}
}
