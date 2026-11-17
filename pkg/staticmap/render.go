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

package staticmap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Errors of a Renderer. They never carry the request URL: it holds the
// coordinates of a message.
var (
	// ErrRenderTimeout: the renderer did not answer in time.
	ErrRenderTimeout = errors.New("staticmap: renderer timed out")
	// ErrRenderFailed: the renderer could not be reached or refused.
	ErrRenderFailed = errors.New("staticmap: renderer failed")
	// ErrBadImage: the answer is not a PNG of the requested size.
	ErrBadImage = errors.New("staticmap: renderer returned a bad image")
)

// maxAnswer bounds an answer of the renderer. A PNG of MaxPixels² is far
// smaller in practice; this only stops a runaway answer.
const maxAnswer = 16 << 20

// Renderer draws the map of p in a style as a PNG of p.PixelWidth() ×
// p.PixelHeight() pixels, without a marker: the clients draw their own
// (tdesktop history_view_location.cpp:551-552, Web K components/wrappers/geo.ts:120-147).
type Renderer interface {
	Render(ctx context.Context, style string, p Params) ([]byte, error)
}

// HTTPRenderer renders with the static maps endpoint of tileserver-gl,
// /styles/{id}/static/{lon},{lat},{zoom}/{w}x{h}[@{s}x].png
// (https://tileserver.readthedocs.io/en/latest/endpoints.html).
type HTTPRenderer struct {
	base    string
	timeout time.Duration
	client  *http.Client
}

// NewHTTPRenderer returns a renderer of the service at base (an http(s) URL
// without a path) whose requests take at most timeout.
func NewHTTPRenderer(base string, timeout time.Duration) *HTTPRenderer {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The service is on the internal network next to bff; an HTTP(S)_PROXY of
	// the environment must not carry the coordinates anywhere else.
	transport.Proxy = nil
	return &HTTPRenderer{
		base:    strings.TrimRight(base, "/"),
		timeout: timeout,
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// URL is the address of the map of p in style. The zoom goes as is:
// tileserver-gl v5.7.0 renders a static map at MapLibre zoom − 1
// (src/render_mode.js:10-13), so its zoom is already the 256-pixel-tile zoom
// the clients mean (Web K components/wrappers/geo.ts:170-172, Android
// ChatMessageCell.java:8776-8780). Longitude goes first.
func (r *HTTPRenderer) URL(style string, p Params) string {
	suffix := ""
	if p.Scale > 1 {
		suffix = fmt.Sprintf("@%dx", p.Scale)
	}
	return fmt.Sprintf("%s/styles/%s/static/%.6f,%.6f,%d/%dx%d%s.png",
		r.base, url.PathEscape(style), p.Long, p.Lat, p.Zoom, p.W, p.H, suffix)
}

// Render implements Renderer.
func (r *HTTPRenderer) Render(ctx context.Context, style string, p Params) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.URL(style, p), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: request: %w", ErrRenderFailed, stripURL(err))
	}
	req.Header.Set("Accept", "image/png")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	defer func() {
		// Drain a little so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		// The body is not logged: tileserver-gl may echo the request.
		return nil, fmt.Errorf("%w: status %d", ErrRenderFailed, resp.StatusCode)
	}
	if mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || mt != "image/png" {
		return nil, fmt.Errorf("%w: content type is not image/png", ErrBadImage)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer+1))
	if err != nil {
		return nil, transportError(err)
	}
	if len(body) > maxAnswer {
		return nil, fmt.Errorf("%w: more than %d bytes", ErrBadImage, maxAnswer)
	}
	if err = checkPNGHeader(body, p); err != nil {
		return nil, err
	}
	return body, nil
}

// checkPNGHeader checks the signature and the IHDR size of a PNG.
func checkPNGHeader(b []byte, p Params) error {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("%w: not a PNG", ErrBadImage)
	}
	if cfg.Width != p.PixelWidth() || cfg.Height != p.PixelHeight() {
		// No sizes in the error: the size of a map is not logged (ADR 0063,
		// p. 8).
		return fmt.Errorf("%w: not of the requested size", ErrBadImage)
	}
	return nil
}

// transportError classifies an error of the HTTP exchange, dropping the URL
// that *url.Error carries.
func transportError(err error) error {
	err = stripURL(err)
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return fmt.Errorf("%w: %w", ErrRenderTimeout, err)
	}
	return fmt.Errorf("%w: %w", ErrRenderFailed, err)
}

func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}
