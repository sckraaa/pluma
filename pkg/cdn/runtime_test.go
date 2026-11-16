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

package cdn

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// runtimeSettings are settings whose origin listens on a free local port
// and whose delivery points at an address nothing listens at.
func runtimeSettings(t *testing.T) *Settings {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	gone := srv.URL
	srv.Close()
	c := testConfig(t)
	c.Deliveries[0].BaseURL = gone
	c.DevAllowHTTP = "1"
	set, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	set.Origin.Listen = "127.0.0.1:0"
	return set
}

// runtimeKey is the permanent auth key of the client of TestRuntime, one
// that sent pluma_cdn.
const runtimeKey = 1

// TestRuntime runs the whole home side: a request of upload.getFile queues
// the file, the preparer woken by it prepares the file, the origin serves
// its cipher, and the stop closes the origin and writes the traffic.
func TestRuntime(t *testing.T) {
	ctx := context.Background()
	set := runtimeSettings(t)
	store := NewMemStore()
	sizer, reader := newFakeFiles(), newMemReader()
	data := refFile()
	key := docKey(42)
	sizer.add(key, data)
	reader.add(key, data)

	caps := &fakeCaps{versions: map[int64]int{runtimeKey: 1}}
	r, err := Start(store, set, sizer, reader, caps)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			r.Stop()
		}
	}()

	if res, result := r.Home.Redirect(ctx, runtimeKey, docLoc(42), 0); res != nil || result != ResultUnhealthy && result != ResultNotReady {
		t.Fatalf("the first request: %v, %q", res, result)
	}
	var f *File
	deadline := time.Now().Add(10 * time.Second)
	for {
		if f, err = store.Get(ctx, key); err != nil {
			t.Fatal(err)
		}
		if f != nil && f.Hashes != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the file was not prepared")
		}
		time.Sleep(10 * time.Millisecond)
	}

	base := "http://" + r.originAddr.String()
	req, _ := http.NewRequest(http.MethodGet, base+ObjectPath(f.Secrets.Token), nil)
	req.Header.Set(OriginAuthHeader, string(set.Origin.AuthValue))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || !bytes.Equal(body, cipherOf(t, f, data)) {
		t.Fatalf("the origin: %d, %d bytes, %v", res.StatusCode, len(body), err)
	}

	// The check of the delivery finds it out: nothing listens there.
	for deadline = time.Now().Add(10 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		h, err := store.DeliveryHealth(ctx, testDc)
		if err != nil {
			t.Fatal(err)
		}
		if h != nil {
			if h.UnhealthyReason != UnhealthyNetwork || h.HealthyUntil != 0 {
				t.Errorf("health: %+v", h)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the delivery was not checked")
		}
	}

	r.Stop()
	stopped = true
	if c, err := net.DialTimeout("tcp", r.originAddr.String(), time.Second); err == nil {
		c.Close()
		t.Error("the origin still listens after the stop")
	}
	u, err := store.Usage(ctx, testDc, UsageMonth(time.Now()))
	if err != nil || u.OriginBytes != int64(len(data)) {
		t.Errorf("traffic written at the stop: %+v, %v; want %d origin bytes", u, err, len(data))
	}
}

func TestRuntimeOriginAddressInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	set := runtimeSettings(t)
	set.Origin.Listen = ln.Addr().String()
	if r, err := Start(NewMemStore(), set, newFakeFiles(), newMemReader(), &fakeCaps{}); err == nil {
		r.Stop()
		t.Fatal("started on an address in use")
	}
}
