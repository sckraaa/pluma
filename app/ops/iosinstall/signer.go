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

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// apple is what the signer needs of App Store Connect (ascClient; a fake
// in the tests).
type apple interface {
	deviceID(ctx context.Context, udid string) (string, error)
	registerDevice(ctx context.Context, name, udid string) (string, error)
	enabledDevices(ctx context.Context) ([]string, error)
	bundleID(ctx context.Context, identifier string) (string, error)
	adHocProfiles(ctx context.Context, bundleID string) ([]ascProfile, error)
	deleteProfile(ctx context.Context, id string) error
	createAdHocProfile(ctx context.Context, name, bundleID, certificateID string, devices []string) ([]byte, error)
}

// signer registers new devices with Apple, issues the ad hoc profiles of
// the app's bundles anew for all the team's devices and re-signs the build
// with them (rcodesign, the certificate's .p12). One run at a time: a
// device that arrives meanwhile waits for the next run, and every device
// gets the same IPA.
type signer struct {
	store       *store
	apple       apple
	dir         string // the volume: builds/, signed/, work/
	rcodesign   string
	p12         string
	p12Password string
	certificate string // the id of the Apple Distribution certificate at Apple
	trigger     chan struct{}
	logf        func(format string, args ...any)
}

func newSigner(st *store, a apple, cfg config) *signer {
	return &signer{
		store:       st,
		apple:       a,
		dir:         cfg.dataDir,
		rcodesign:   cfg.rcodesign,
		p12:         cfg.p12File,
		p12Password: cfg.p12PasswordFile,
		certificate: cfg.certificateID,
		trigger:     make(chan struct{}, 1),
		logf:        log.Printf,
	}
}

// kick asks for a run.
func (s *signer) kick() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// loop runs on every kick and retries a failed run with a growing pause.
func (s *signer) loop(ctx context.Context) {
	retry := time.Duration(0)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	s.kick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.trigger:
		case <-timer.C:
		}
		err := s.run(ctx)
		if err == nil {
			retry = 0
			continue
		}
		if ctx.Err() != nil {
			return
		}
		retry = min(max(2*retry, time.Minute), 30*time.Minute)
		s.logf("iosinstall: signing failed, again in %v: %v", retry, err)
		timer.Reset(retry)
	}
}

// run registers the waiting devices and re-signs the build if its profiles
// lack a device.
func (s *signer) run(ctx context.Context) error {
	if err := s.registerWaiting(ctx); err != nil {
		return err
	}
	st := s.store.view()
	if st.Build == nil {
		return nil
	}
	devices, err := s.apple.enabledDevices(ctx)
	if err != nil {
		return err
	}
	devices = sortedCopy(devices)
	if st.Signed != nil && st.Signed.Build == st.Build.ID && slices.Equal(st.Signed.Devices, devices) {
		return nil
	}
	profiles, err := s.issueProfiles(ctx, st.Build, devices)
	if err != nil {
		return err
	}
	file, err := s.resign(ctx, st.Build, profiles)
	if err != nil {
		return err
	}
	var old string
	err = s.store.update(func(st *state) error {
		if st.Signed != nil {
			old = st.Signed.File
		}
		st.Signed = &Signed{Build: st.Build.ID, File: file, Devices: devices, Signed: time.Now().UTC()}
		return nil
	})
	if err != nil {
		s.removeSigned(file)
		return err
	}
	if old != "" && old != file {
		s.removeSigned(old)
	}
	s.logf("iosinstall: build %s %s (%s) signed for %d devices", short(st.Build.ID), st.Build.Version, st.Build.Number, len(devices))
	return nil
}

// removeSigned removes an IPA of signed/; names come from the state file,
// so through the directory's root.
func (s *signer) removeSigned(name string) {
	root, err := os.OpenRoot(filepath.Join(s.dir, "signed"))
	if err != nil {
		return
	}
	defer root.Close()
	_ = root.Remove(name)
}

// registerWaiting gives every new device its id at Apple, registering it
// if the team has not yet.
func (s *signer) registerWaiting(ctx context.Context) error {
	for _, d := range s.store.view().Devices {
		if d.AppleID != "" || d.Failure != "" {
			continue
		}
		id, err := s.apple.deviceID(ctx, d.UDID)
		if err == nil && id == "" {
			id, err = s.apple.registerDevice(ctx, deviceName(d), d.UDID)
		}
		var refused *ascError
		if errors.As(err, &refused) && refused.Status/100 == 4 && refused.Status != 401 && refused.Status != 403 && refused.Status != 429 {
			// Apple refuses this device (a malformed UDID, the yearly
			// limit): the operator sees why, the others go on.
			// Apple's answer may quote the UDID: only its status is logged.
			s.logf("iosinstall: Apple refused a device (%s): HTTP %d", d.Product, refused.Status)
			udid := d.UDID
			if err := s.store.update(func(st *state) error {
				if dev := st.device(udid); dev != nil {
					dev.Failure = truncate(refused.Body, 500)
				}
				return nil
			}); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		udid := d.UDID
		if err := s.store.update(func(st *state) error {
			if dev := st.device(udid); dev != nil {
				dev.AppleID = id
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// deviceName is the device's name in the team's list: no person's name,
// the model and the day it came.
func deviceName(d *Device) string {
	return truncate(fmt.Sprintf("Pluma %s %s", d.Product, d.Added.Format("2006-01-02")), 50)
}

// issueProfiles replaces the ad hoc profiles of every bundle with new ones
// for devices and returns their contents by bundle path. The old profile's
// name is kept, so the team's list reads as before.
func (s *signer) issueProfiles(ctx context.Context, b *Build, devices []string) (map[string][]byte, error) {
	out := make(map[string][]byte, len(b.Bundles))
	for _, bundle := range b.Bundles {
		id, err := s.apple.bundleID(ctx, bundle.BundleID)
		if err != nil {
			return nil, err
		}
		old, err := s.apple.adHocProfiles(ctx, id)
		if err != nil {
			return nil, err
		}
		name := "Pluma Ad Hoc " + bundle.BundleID
		for _, p := range old {
			name = p.Name
			if err := s.apple.deleteProfile(ctx, p.ID); err != nil {
				return nil, err
			}
		}
		content, err := s.apple.createAdHocProfile(ctx, name, id, s.certificate, devices)
		if err != nil {
			return nil, err
		}
		out[bundle.Path] = content
	}
	return out, nil
}

// resign signs the build anew with the profiles and returns the name of the
// IPA in signed/. The IPA is unpacked into a temporary directory and every
// file of it is reached through that directory's os.Root: no name in the
// archive leads outside.
func (s *signer) resign(ctx context.Context, b *Build, profiles map[string][]byte) (string, error) {
	work, err := os.MkdirTemp(filepath.Join(s.dir, "work"), "sign-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	root, err := os.OpenRoot(work)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := unzip(filepath.Join(s.dir, "builds", b.ID+".ipa"), root); err != nil {
		return "", err
	}
	app, err := payloadApp(root)
	if err != nil {
		return "", err
	}
	args := []string{"sign", "--p12-file", s.p12, "--p12-password-file", s.p12Password}
	for i, bundle := range b.Bundles {
		// The profile is a file of the app the device reads, as CI's.
		if err := root.WriteFile(path.Join(app, bundle.Path, "embedded.mobileprovision"), profiles[bundle.Path], 0o644); err != nil {
			return "", err
		}
		ent := fmt.Sprintf("entitlements-%d.plist", i)
		if err := root.WriteFile(ent, []byte(bundle.Entitlements), 0o600); err != nil {
			return "", err
		}
		if bundle.Path == "" {
			args = append(args, "-e", filepath.Join(work, ent))
		} else {
			args = append(args, "-e", bundle.Path+":"+filepath.Join(work, ent))
		}
	}
	args = append(args, filepath.Join(work, filepath.FromSlash(app)))
	cctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.rcodesign, args...) //nolint:gosec // G204: the operator's rcodesign, our arguments
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rcodesign: %w: %s", err, truncate(lastLines(output.String(), 15), 2000))
	}

	signed, err := os.OpenRoot(filepath.Join(s.dir, "signed"))
	if err != nil {
		return "", err
	}
	defer signed.Close()
	name := fmt.Sprintf("%s-%d.ipa", short(b.ID), time.Now().Unix())
	if err := zipDir(root, "Payload", signed, name+".tmp"); err != nil {
		_ = signed.Remove(name + ".tmp")
		return "", err
	}
	if err := signed.Rename(name+".tmp", name); err != nil {
		return "", err
	}
	return name, nil
}

// payloadApp returns the app bundle of an unpacked IPA, relative to it.
func payloadApp(root *os.Root) (string, error) {
	apps, _ := fs.Glob(root.FS(), "Payload/*.app")
	if len(apps) != 1 {
		return "", fmt.Errorf("IPA: %d apps in Payload", len(apps))
	}
	return apps[0], nil
}

// unzip unpacks an IPA into root: regular files and directories only.
func unzip(src string, root *os.Root) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		name := path.Clean(f.Name)
		if !fs.ValidPath(name) || name == "." {
			return fmt.Errorf("IPA: bad path %q", f.Name)
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := root.MkdirAll(name, 0o700); err != nil {
				return err
			}
		case mode.IsRegular():
			if err := root.MkdirAll(path.Dir(name), 0o700); err != nil {
				return err
			}
			if err := extract(f, root, name, mode.Perm()|0o600); err != nil {
				return err
			}
		default:
			return fmt.Errorf("IPA: %q is no regular file", f.Name)
		}
	}
	return nil
}

func extract(f *zip.File, root *os.Root, name string, perm fs.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, io.LimitReader(rc, 2<<30)); err == nil {
		err = out.Close()
	} else {
		out.Close()
	}
	return err
}

// zipDir packs dir of src into the IPA name in dst, with the files' modes
// (an app's executables keep theirs).
func zipDir(src *os.Root, dir string, dst *os.Root, name string) error {
	out, err := dst.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	err = fs.WalkDir(src.FS(), dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		h, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		h.Name = p
		if d.IsDir() {
			h.Name += "/"
			_, err = zw.CreateHeader(h)
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("IPA: %q is no regular file", p)
		}
		h.Method = zip.Deflate
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		f, err := src.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ipaBundles lists the app and extension bundles of an IPA by their path
// in the app ("" for the app): those whose code the profiles sign.
func ipaBundles(ipaPath string) (map[string]bool, error) {
	r, err := zip.OpenReader(ipaPath)
	if err != nil {
		return nil, fmt.Errorf("IPA: %w", err)
	}
	defer r.Close()
	out := map[string]bool{}
	apps := map[string]bool{}
	for _, f := range r.File {
		parts := strings.Split(path.Clean(f.Name), "/")
		if len(parts) < 3 || parts[0] != "Payload" || !strings.HasSuffix(parts[1], ".app") {
			continue
		}
		apps[parts[1]] = true
		switch {
		case len(parts) == 3 && parts[2] == "Info.plist":
			out[""] = true
		case len(parts) == 5 && parts[2] == "PlugIns" && strings.HasSuffix(parts[3], ".appex") && parts[4] == "Info.plist":
			out["PlugIns/"+parts[3]] = true
		}
	}
	if len(apps) != 1 || !out[""] {
		return nil, fmt.Errorf("IPA: %d apps in Payload", len(apps))
	}
	return out, nil
}
