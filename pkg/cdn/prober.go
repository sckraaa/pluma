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
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"

	"github.com/teamgram/teamgram-server/pkg/mtserver/codec"
)

// Rhythm of the prober (task 102).
const (
	// probeEvery is how often the node is checked; a healthy node takes
	// redirects for healthyFor after the check, so a node that stops
	// answering is out within healthyFor.
	probeEvery = 10 * time.Second
	healthyFor = 30 * time.Second
	// handshakeTimeout bounds the check of the node's MTProto port.
	handshakeTimeout = 3 * time.Second
	// inventoryEvery is how often the copies at home are compared with the
	// files on the node.
	inventoryEvery = 10 * time.Minute
	// inventoryPage is how many ready copies are read at once.
	inventoryPage = 500
	// maxInventoryFiles bounds the files of an inventory kept in memory.
	maxInventoryFiles = 1 << 20
	// rtKeyEvery is how often the request_token key is replaced; the one
	// before stays valid, and a token lives ReqTokenTTL.
	rtKeyEvery = 30 * 24 * time.Hour
)

// Reasons a node is out (cdn_nodes.unhealthy_reason).
const (
	unhealthyUnreachable = "unreachable"
	unhealthyDcMismatch  = "dc_mismatch"
	unhealthyNoKey       = "no_key"
	unhealthyDiskLow     = "disk_low"
	unhealthyEgress      = "egress_limit"
	unhealthyKeysUnset   = "request_keys_unset"
	unhealthyMTProto     = "mtproto_unreachable"
	unhealthyFingerprint = "fingerprint_mismatch"
)

// Prober checks the node (ADR 0065 §6): its control API answers, it is the
// DC configured, its disk and its monthly traffic are within bounds, and
// its public MTProto port answers a key exchange with the key the control
// API names. It records what it saw in cdn_nodes (healthy_until), keeps the
// node's request_token keys current, and compares the copies at home with
// the files on the node.
type Prober struct {
	store Store
	node  NodeAPI
	set   *Settings
	now   func() time.Time
	dial  func(ctx context.Context, addr string) (net.Conn, error)

	mu       sync.Mutex
	healthy  bool
	cacheCap int64
	sentKey  []byte // the request_token key this process last handed the node

	lastInventory time.Time
}

// NewProber returns the prober of the node of set.
func NewProber(store Store, node NodeAPI, set *Settings) *Prober {
	var d net.Dialer
	return &Prober{store: store, node: node, set: set, now: time.Now, dial: func(ctx context.Context, addr string) (net.Conn, error) {
		return d.DialContext(ctx, "tcp", addr)
	}}
}

// Healthy reports whether the last check found the node healthy.
func (p *Prober) Healthy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.healthy
}

// CacheCap is the node's cache in bytes as it last reported it, 0 if not
// known.
func (p *Prober) CacheCap() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cacheCap
}

// Run checks the node now and every probeEvery until ctx is done, and
// takes its inventory every inventoryEvery while it is healthy.
func (p *Prober) Run(ctx context.Context) {
	t := time.NewTicker(probeEvery)
	defer t.Stop()
	for {
		p.round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (p *Prober) round(ctx context.Context) {
	if err := p.probe(ctx); err != nil && ctx.Err() == nil {
		logx.WithContext(ctx).Errorf("cdn: check of node %d: %v", p.set.DcId, err)
	}
	if !p.Healthy() || p.now().Sub(p.lastInventory) < inventoryEvery {
		return
	}
	if err := p.inventory(ctx); err != nil {
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorf("cdn: inventory of node %d: %v", p.set.DcId, err)
		}
		return
	}
	p.lastInventory = p.now()
}

// probe checks the node once and records the outcome. An error is a
// failure of the home (MySQL); a node that fails a check is out, with its
// reason, and is not an error.
func (p *Prober) probe(ctx context.Context) error {
	now := p.now()
	ms := now.UnixMilli()
	dc := dcLabel(p.set.DcId)
	metricNodeEgressLimit.Set(float64(p.set.MonthlyEgressLimit), dc)

	row, err := p.store.Node(ctx, p.set.DcId)
	if err == nil && (row == nil || len(row.RtKey) == 0 || ms-row.RtKeyAt >= rtKeyEvery.Milliseconds()) {
		var key []byte
		if key, err = NewRtKey(); err == nil {
			row, err = p.store.RotateRtKey(ctx, p.set.DcId, key, rtKeyEvery, ms)
		}
	}
	if err != nil {
		p.setHealthy(false, 0)
		metricNodeHealthy.Set(0, dc)
		return err
	}

	n := *row
	n.UpdatedAt = ms
	reason, cacheCap := p.check(ctx, &n, row)
	if reason == "" {
		n.HealthyUntil, n.UnhealthyReason = ms+healthyFor.Milliseconds(), ""
	} else {
		n.HealthyUntil, n.UnhealthyReason = 0, reason
	}
	if err = p.store.PutNode(ctx, &n); err != nil {
		p.setHealthy(false, cacheCap)
		metricNodeHealthy.Set(0, dc)
		return err
	}
	p.setHealthy(reason == "", cacheCap)
	if reason == "" {
		metricNodeHealthy.Set(1, dc)
	} else {
		metricNodeHealthy.Set(0, dc)
	}

	counts, err := p.store.QueueCounts(ctx, p.set.DcId)
	if err != nil {
		return err
	}
	for state, name := range stateNames {
		metricQueue.Set(float64(counts[state]), name)
	}
	return nil
}

// check asks the node and fills n with what it said; it returns why the
// node is out, empty if it is healthy, and its cache.
func (p *Prober) check(ctx context.Context, n, row *Node) (string, int64) {
	st, err := p.node.Status(ctx)
	if err != nil {
		logx.WithContext(ctx).Infof("cdn: node %d: status: %v", p.set.DcId, err)
		return unhealthyUnreachable, 0
	}
	n.PublicKey, n.Fingerprint, n.BootId = st.GetPublicKeyPem(), st.GetFingerprint(), st.GetBootId()
	n.EgressMonth, n.EgressBytes = st.GetEgressMonth(), st.GetEgressBytes()
	n.CacheUsed, n.CacheCap = st.GetCacheUsed(), st.GetCacheCap()
	if n.EgressMonth == p.now().UTC().Format("2006-01") {
		metricNodeEgress.Set(float64(n.EgressBytes), dcLabel(p.set.DcId))
	}

	switch {
	case st.GetDcId() != p.set.DcId:
		return unhealthyDcMismatch, n.CacheCap
	case n.PublicKey == "":
		return unhealthyNoKey, n.CacheCap
	case st.GetDiskLow():
		return unhealthyDiskLow, n.CacheCap
	case n.EgressMonth == p.now().UTC().Format("2006-01") && n.EgressBytes >= p.set.MonthlyEgressLimit:
		return unhealthyEgress, n.CacheCap
	}

	// The node keeps the key in memory only: after its restart it asks
	// for it again by request_token_keys_set, and after a rotation the key
	// differs from the one handed it.
	if !st.GetRequestTokenKeysSet() || !bytes.Equal(row.RtKey, p.lastKey()) {
		if err := p.node.SetRequestTokenKeys(ctx, row.RtKey, row.RtKeyPrev); err != nil {
			logx.WithContext(ctx).Errorf("cdn: node %d: hand the request_token keys: %v", p.set.DcId, err)
			return unhealthyKeysUnset, n.CacheCap
		}
		p.mu.Lock()
		p.sentKey = row.RtKey
		p.mu.Unlock()
	}

	fps, err := p.resPQ(ctx)
	if err != nil {
		logx.WithContext(ctx).Infof("cdn: node %d: key exchange at the public address: %v", p.set.DcId, err)
		return unhealthyMTProto, n.CacheCap
	}
	if !slices.Contains(fps, n.Fingerprint) {
		// Another server answers at the address, or the node's key changed
		// between the two checks; either way clients would fail.
		return unhealthyFingerprint, n.CacheCap
	}
	return "", n.CacheCap
}

func (p *Prober) lastKey() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sentKey
}

func (p *Prober) setHealthy(ok bool, cacheCap int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.healthy = ok
	if cacheCap > 0 {
		p.cacheCap = cacheCap
	}
}

// resPQ opens a connection to the public address of the node as a client
// given the DC's secret does, sends req_pq_multi and returns the key
// fingerprints of resPQ.
func (p *Prober) resPQ(ctx context.Context) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	conn, err := p.dial(ctx, p.set.DcAddress)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if d, ok := ctx.Deadline(); ok {
		if err = conn.SetDeadline(d); err != nil {
			return nil, err
		}
	}
	t, err := newObfuscatedClient(conn, p.set.DcSecret, int16(p.set.DcId))
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	x := mtproto.NewEncodeBuf(32)
	if err = (&mtproto.TLReqPqMulti{Constructor: mtproto.CRC32_req_pq_multi, Nonce: nonce}).Encode(x, 0); err != nil {
		return nil, err
	}
	body := x.GetBuf()
	msg := make([]byte, 20+len(body))
	now := p.now()
	msgId := (now.Unix()<<32 | int64(uint64(now.Nanosecond())<<32/1e9)) &^ 3
	binary.LittleEndian.PutUint64(msg[8:], uint64(msgId))
	binary.LittleEndian.PutUint32(msg[16:], uint32(len(body)))
	copy(msg[20:], body)
	if err = t.writePacket(msg); err != nil {
		return nil, err
	}

	ans, err := t.readPacket()
	if err != nil {
		return nil, err
	}
	if len(ans) < 20 || binary.LittleEndian.Uint64(ans) != 0 {
		return nil, errors.New("the answer is not an unencrypted message")
	}
	n := binary.LittleEndian.Uint32(ans[16:])
	if uint64(n) > uint64(len(ans)-20) {
		return nil, errors.New("the answer has a bad length")
	}
	d := mtproto.NewDecodeBuf(ans[20 : 20+n])
	o := d.Object()
	if err = d.GetError(); err != nil {
		return nil, err
	}
	res, ok := o.(*mtproto.TLResPQ)
	if !ok {
		return nil, fmt.Errorf("the answer is %T, not resPQ", o)
	}
	if !bytes.Equal(res.GetNonce(), nonce) {
		return nil, errors.New("resPQ with another nonce")
	}
	return res.GetServerPublicKeyFingerprints(), nil
}

// obfuscatedClient is the client side of the obfuscated transport with a
// "dd" secret, as the clients open a connection to a DC whose dcOption
// carries one (pkg/mtserver/codec/secret.go names their code): a 64-byte
// header whose keys are SHA256(key + secret), then the padded intermediate
// transport.
type obfuscatedClient struct {
	conn net.Conn
	r    *bufio.Reader
	send cipher.Stream
	recv cipher.Stream
}

// paddedTag is the tag of the padded intermediate transport in the header.
const paddedTag = 0xdddddddd

// maxProbePacket bounds an answer to req_pq_multi.
const maxProbePacket = 64 << 10

func newObfuscatedClient(conn net.Conn, secret codec.Secret, dc int16) (*obfuscatedClient, error) {
	header := make([]byte, 64)
	for {
		if _, err := rand.Read(header); err != nil {
			return nil, err
		}
		// A header that reads as another transport is drawn again, as the
		// clients do (Android Connection.cpp:524-527).
		first := binary.LittleEndian.Uint32(header)
		if header[0] != 0xef && binary.LittleEndian.Uint32(header[4:]) != 0 &&
			first != 0x44414548 && first != 0x54534f50 && first != 0x20544547 && first != 0x4954504f &&
			first != 0xeeeeeeee && first != paddedTag && first != 0x02010316 {
			break
		}
	}
	binary.LittleEndian.PutUint32(header[56:], paddedTag)
	binary.LittleEndian.PutUint16(header[60:], uint16(dc))
	reversed := make([]byte, 48)
	for i := range reversed {
		reversed[i] = header[55-i]
	}
	send, err := secretStream(header[8:40], header[40:56], secret)
	if err != nil {
		return nil, err
	}
	recv, err := secretStream(reversed[:32], reversed[32:48], secret)
	if err != nil {
		return nil, err
	}
	encrypted := make([]byte, 64)
	send.XORKeyStream(encrypted, header)
	copy(header[56:], encrypted[56:])
	if _, err = conn.Write(header); err != nil {
		return nil, err
	}
	return &obfuscatedClient{conn: conn, r: bufio.NewReader(conn), send: send, recv: recv}, nil
}

func secretStream(key, iv []byte, secret codec.Secret) (cipher.Stream, error) {
	h := sha256.New()
	h.Write(key)
	h.Write(secret[:])
	block, err := aes.NewCipher(h.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, iv), nil
}

func (o *obfuscatedClient) writePacket(p []byte) error {
	var pad [1]byte
	if _, err := rand.Read(pad[:]); err != nil {
		return err
	}
	n := int(pad[0] % 16)
	b := make([]byte, 4+len(p)+n)
	binary.LittleEndian.PutUint32(b, uint32(len(p)+n))
	copy(b[4:], p)
	if _, err := rand.Read(b[4+len(p):]); err != nil {
		return err
	}
	o.send.XORKeyStream(b, b)
	_, err := o.conn.Write(b)
	return err
}

func (o *obfuscatedClient) readPacket() ([]byte, error) {
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(o.r, hdr[:]); err != nil {
			return nil, err
		}
		o.recv.XORKeyStream(hdr[:], hdr[:])
		n := binary.LittleEndian.Uint32(hdr[:])
		if n&0x80000000 != 0 {
			continue // a quick ACK
		}
		if n < 4 || n > maxProbePacket {
			return nil, fmt.Errorf("a packet of %d bytes", n)
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(o.r, b); err != nil {
			return nil, err
		}
		o.recv.XORKeyStream(b, b)
		if n == 4 {
			return nil, fmt.Errorf("transport error %d", int32(binary.LittleEndian.Uint32(b)))
		}
		return b, nil
	}
}

// inventory compares the ready copies at home with the files on the node:
// a copy the node does not have (or has with another size) is made absent,
// and is queued again by the next request of its file; a file on the node
// no copy at home accounts for (a push cut short, a row deleted) is
// deleted from the node.
func (p *Prober) inventory(ctx context.Context) error {
	start := p.now().UnixMilli()
	onNode := map[string]int64{}
	if err := p.node.Inventory(ctx, func(token []byte, size int64) error {
		if len(onNode) >= maxInventoryFiles {
			return fmt.Errorf("more than %d files", maxInventoryFiles)
		}
		onNode[string(token)] = size
		return nil
	}); err != nil {
		return err
	}

	ready := map[string]bool{}
	var missing []int64
	for after := int64(0); ; {
		page, err := p.store.ListReady(ctx, p.set.DcId, after, inventoryPage)
		if err != nil {
			return err
		}
		for _, c := range page {
			ready[string(c.Token)] = true
			if size, ok := onNode[string(c.Token)]; !ok || size != c.Size {
				missing = append(missing, c.FileRowId)
			}
			after = c.FileRowId
		}
		if len(page) < inventoryPage {
			break
		}
	}
	absent := 0
	for _, rowId := range missing {
		r, err := p.store.Replica(ctx, rowId, p.set.DcId)
		if err != nil {
			return err
		}
		// A copy made ready after the node listed its files may simply
		// not be in the list.
		if r == nil || r.State != StateReady || r.ReadyAt >= start {
			continue
		}
		ok, err := p.store.MarkAbsent(ctx, rowId, p.set.DcId, p.now().UnixMilli())
		if err != nil {
			return err
		}
		if ok {
			absent++
		}
	}

	deleted := 0
	for token := range onNode {
		if ready[token] {
			continue
		}
		f, err := p.store.ByToken(ctx, []byte(token))
		if err != nil {
			return err
		}
		if f != nil {
			r, err := p.store.Replica(ctx, f.RowId, p.set.DcId)
			if err != nil {
				return err
			}
			// A push in progress or about to be made ready, or one made
			// ready after the list was read: not an orphan.
			if r != nil && (r.State == StatePushing || r.State == StateQueued || r.State == StateReady) {
				continue
			}
		}
		if _, err = p.node.Delete(ctx, []byte(token)); err != nil {
			return err
		}
		deleted++
	}
	if absent > 0 || deleted > 0 {
		logx.WithContext(ctx).Infof("cdn: inventory of node %d: %d copies absent, %d files deleted", p.set.DcId, absent, deleted)
	}
	return nil
}
