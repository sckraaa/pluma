// Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
//  All rights reserved.
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package dao

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/teamgram/teamgram-server/app/interface/gnetway/internal/config"
	sessionclient "github.com/teamgram/teamgram-server/app/interface/session/client"

	"github.com/zeromicro/go-zero/core/discov"
	"github.com/zeromicro/go-zero/core/hash"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stringx"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxNodeFailures = 3 // consecutive failures before removing node from ring
	// nodeRetryAfter is how long a node out of the ring after failures stays
	// out before it is tried again, while etcd still lists it (pluma: it was
	// out for good, and with a single session node the gateway answered
	// every connection "not found session" until restarted).
	nodeRetryAfter = 10 * time.Second
)

var (
	ErrSessionNotFound = errors.New("not found session")
)

type ShardingSessionClient struct {
	mu           sync.RWMutex
	gatewayId    string
	dispatcher   *hash.ConsistentHash
	sessions     map[string]sessionclient.SessionClient // the nodes etcd lists
	inRing       map[string]bool                        // those the ring routes to
	failCounters map[string]int
	retryAfter   time.Duration
}

func NewShardingSessionClient(c config.Config) *ShardingSessionClient {
	sess := &ShardingSessionClient{
		dispatcher:   hash.NewConsistentHash(),
		sessions:     make(map[string]sessionclient.SessionClient),
		inRing:       make(map[string]bool),
		failCounters: make(map[string]int),
		retryAfter:   nodeRetryAfter,
	}
	sess.watch(c.Session)

	return sess
}

func (sess *ShardingSessionClient) watch(c zrpc.RpcClientConf) {
	sub, err := discov.NewSubscriber(c.Etcd.Hosts, c.Etcd.Key)
	if err != nil {
		logx.Errorf("watchSession NewSubscriber(hosts: %v, key: %s) error: %v", c.Etcd.Hosts, c.Etcd.Key, err)
		return
	}
	update := func() {
		var (
			removeClis []string
		)

		sess.mu.Lock()
		defer sess.mu.Unlock()

		values := sub.Values()
		sessions := map[string]sessionclient.SessionClient{}
		for _, v := range values {
			if old, ok := sess.sessions[v]; ok {
				sessions[v] = old
				continue
			}
			c.Endpoints = []string{v}
			cli, err := zrpc.NewClient(
				c,
				zrpc.WithDialOption(grpc.WithReadBufferSize(16*1024*1024)),
				zrpc.WithDialOption(grpc.WithWriteBufferSize(16*1024*1024)),
			)
			if err != nil {
				logx.Errorf("watchSession NewClient(%v) error: %v", v, err)
				continue
			}
			sessionCli := sessionclient.NewSessionClient(cli)
			sessions[v] = sessionCli
		}

		for key, _ := range sess.sessions {
			if !stringx.Contains(values, key) {
				removeClis = append(removeClis, key)
			}
		}

		for _, n := range removeClis {
			sess.dispatcher.Remove(n)
			delete(sess.inRing, n)
			delete(sess.failCounters, n)
		}

		sess.sessions = sessions

		// Every node etcd lists is in the ring, a node taken out after
		// failures too.
		for n := range sessions {
			sess.addToRing(n)
		}
	}

	sub.AddListener(update)
	update()
}

func (sess *ShardingSessionClient) InvokeByKey(key string, cb func(client sessionclient.SessionClient) (err error)) error {
	if cb == nil {
		return nil
	}

	sess.mu.RLock()
	val, ok := sess.dispatcher.Get(key)
	if !ok {
		sess.mu.RUnlock()
		return ErrSessionNotFound
	}

	node := val.(string)
	cli, ok := sess.sessions[node]
	sess.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	err := cb(cli)
	if err == nil {
		// reset failure counter on success
		sess.mu.Lock()
		delete(sess.failCounters, node)
		sess.mu.Unlock()
		return nil
	}

	// redirect 错误：session 告知正确节点，直接重路由
	if target, ok := parseRedirectError(err); ok {
		logx.Infof("session node %s redirected to %s for key %s", node, target, key)

		sess.mu.RLock()
		redirectCli, ok := sess.sessions[target]
		sess.mu.RUnlock()

		if ok {
			return cb(redirectCli)
		}
		// 目标节点不在已知列表中，按连接错误处理走 fallback
	}

	// 非连接错误直接返回
	if !isConnError(err) {
		return err
	}

	// 连接错误：增加失败计数，达到阈值才从哈希环移除
	// pluma: not the last node of the ring, which has no other to take its
	// keys, and only for a while: the node keeps its client and comes back
	// after retryAfter if etcd still lists it.
	sess.mu.Lock()
	sess.failCounters[node]++
	failCount := sess.failCounters[node]
	if failCount >= maxNodeFailures && len(sess.inRing) > 1 {
		logx.Errorf("session node %s unreachable (%d consecutive failures), out of the ring for %s", node, failCount, sess.retryAfter)
		sess.dispatcher.Remove(node)
		delete(sess.inRing, node)
		delete(sess.failCounters, node)
		time.AfterFunc(sess.retryAfter, func() {
			sess.restore(node)
		})
	} else {
		logx.Errorf("session node %s connection error (%d/%d), retrying on another node", node, failCount, maxNodeFailures)
	}
	sess.mu.Unlock()

	sess.mu.RLock()
	newVal, ok := sess.dispatcher.Get(key)
	if !ok {
		sess.mu.RUnlock()
		return ErrSessionNotFound
	}
	newNode := newVal.(string)
	newCli, ok := sess.sessions[newNode]
	sess.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	return cb(newCli)
}

// addToRing puts a listed node in the ring unless it is there; the caller
// holds mu.
func (sess *ShardingSessionClient) addToRing(node string) {
	if !sess.inRing[node] {
		sess.dispatcher.Add(node)
		sess.inRing[node] = true
	}
}

// restore puts a node taken out after failures back in the ring, if etcd
// still lists it.
func (sess *ShardingSessionClient) restore(node string) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if _, ok := sess.sessions[node]; ok && !sess.inRing[node] {
		logx.Infof("session node %s back in the ring", node)
		sess.addToRing(node)
	}
}

// parseRedirectError 解析 session 返回的 redirect 错误，提取目标节点地址
// 错误格式：gRPC code 700, message "REDIRECT_TO_{server_addr}"
func parseRedirectError(err error) (string, bool) {
	s, ok := status.FromError(err)
	if !ok {
		return "", false
	}
	if s.Code() != codes.Code(700) {
		return "", false
	}
	msg := s.Message()
	const prefix = "REDIRECT_TO_"
	if strings.HasPrefix(msg, prefix) {
		target := msg[len(prefix):]
		if target != "" {
			return target, true
		}
	}
	return "", false
}

// isConnError 判断是否为连接级错误（节点不可达）
// pluma: a deadline exceeded is a slow answer, not a node out of reach: under
// load three of them took the only session node out of the ring.
func isConnError(err error) bool {
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	return s.Code() == codes.Unavailable
}
