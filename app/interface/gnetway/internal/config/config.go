/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package config

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/teamgram/marmota/pkg/container2"
	"github.com/zeromicro/go-zero/zrpc"
)

type Config struct {
	zrpc.RpcServerConf
	RSAKey           []RSAKey
	Gnetway          *GnetwayConfig
	Session          zrpc.RpcClientConf
	UseStreamSession bool `json:",default=false"`
}

type RSAKey struct {
	KeyFile        string
	KeyFingerprint string
}

type GnetwayServer struct {
	Proto string `json:",default=tcp,options=tcp|websocket|http"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	PPV1  bool   `json:",optional"`                               //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Edge marks the entry behind the CDN edges (task 109, ADR 0072): a
	// WebSocket listener whose client address comes in the
	// X-Pluma-Client-Addr header of the upgrade request, which the origin
	// proxy sets. A connection without it is closed; the gateway pings the
	// client so that the CDN keeps an idle connection.
	Edge      bool `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	Addresses []string
}

type GnetwayConfig struct {
	Server        []GnetwayServer
	Multicore     bool
	SendBuf       int
	ReceiveBuf    int
	AuthKeyCacheM int `json:",default=10"`
	// MaxConnsPerIP and KeyExchangesPerIPMinute bound one client address
	// (IPv6: its /64): the connections it holds and the req_DH_params it
	// sends a minute (task 008). 0 takes the defaults, 512 and 300.
	MaxConnsPerIP           int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	KeyExchangesPerIPMinute int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// EdgePingInterval is how often the Edge entry pings an upgraded
	// connection: Gcore closes a WebSocket after 30 s without data from the
	// origin (ADR 0072, "Gcore"), so it must stay below that. 5s..25s.
	EdgePingInterval time.Duration `json:",default=20s"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// EdgeVias are the values of X-Pluma-Edge-Via (set by the origin proxy
	// of each edge) that label the metrics of the Edge entry; any other
	// value, or none, is "other". Empty takes gcore and ru.
	EdgeVias []string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Bounds of GnetwayConfig.EdgePingInterval.
const (
	MinEdgePingInterval = 5 * time.Second
	MaxEdgePingInterval = 25 * time.Second
)

// EdgeViaOther labels the Edge connections that came through none of
// EdgeVias.
const EdgeViaOther = "other"

var defaultEdgeVias = []string{"gcore", "ru"}

var edgeViaRe = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// Validate checks the listeners and the Edge options; the gateway does not
// start with an error.
func (c GnetwayConfig) Validate() error {
	for _, server := range c.Server {
		if server.Edge {
			if server.Proto != "websocket" {
				return fmt.Errorf("gnetway: an Edge listener must be Proto websocket, not %q", server.Proto)
			}
			if server.PPV1 {
				// The address of an Edge connection comes in a header the
				// origin proxy sets; a PROXY header would be a second source.
				return errors.New("gnetway: a listener cannot be both Edge and PPV1")
			}
		}
	}
	if c.EdgePingInterval < MinEdgePingInterval || c.EdgePingInterval > MaxEdgePingInterval {
		return fmt.Errorf("gnetway: EdgePingInterval %s is outside %s..%s", c.EdgePingInterval, MinEdgePingInterval, MaxEdgePingInterval)
	}
	vias := map[string]bool{}
	for _, via := range c.EdgeVias {
		if !edgeViaRe.MatchString(via) || via == EdgeViaOther {
			return fmt.Errorf("gnetway: EdgeVias: %q is not a name of 1-16 of a-z0-9 other than %q", via, EdgeViaOther)
		}
		if vias[via] {
			return fmt.Errorf("gnetway: EdgeVias: %q is listed twice", via)
		}
		vias[via] = true
	}
	return nil
}

// EdgeViaNames returns EdgeVias, or the defaults when it is empty.
func (c GnetwayConfig) EdgeViaNames() []string {
	if len(c.EdgeVias) == 0 {
		return defaultEdgeVias
	}
	return c.EdgeVias
}

func (c GnetwayConfig) IsEdge(addr string) bool {
	for _, server := range c.Server {
		if server.Edge {
			for _, address := range server.Addresses {
				if address == addr {
					return true
				}
			}
		}
	}
	return false
}

func (c GnetwayConfig) IsWebsocket(addr string) bool {
	for _, server := range c.Server {
		if server.Proto == "websocket" {
			for _, address := range server.Addresses {
				if address == addr {
					return true
				}
			}
		}
	}
	return false
}

func (c GnetwayConfig) IsHttp(addr string) bool {
	for _, server := range c.Server {
		if server.Proto == "http" {
			for _, address := range server.Addresses {
				if address == addr {
					return true
				}
			}
		}
	}
	return false
}

func (c GnetwayConfig) IsTcp(addr string) bool {
	for _, server := range c.Server {
		if server.Proto == "tcp" {
			for _, address := range server.Addresses {
				if address == addr {
					return true
				}
			}
		}
	}
	return false
}

func (c GnetwayConfig) IsProxyProtocolV1(addr string) bool {
	for _, server := range c.Server {
		if server.PPV1 {
			for _, address := range server.Addresses {
				if address == addr {
					return true
				}
			}
		}
	}
	return false
}

func (c GnetwayConfig) ToAddresses() []string {
	var addresses []string
	for _, server := range c.Server {
		for _, address := range server.Addresses {
			if ok := container2.ContainsString(addresses, address); !ok {
				addresses = append(addresses, "tcp://"+address)
			}
		}
	}
	return addresses
}
