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

package transport

import (
	"fmt"

	"github.com/pion/logging"
	"github.com/zeromicro/go-zero/core/logx"
)

// pionLogger passes warnings and errors of the pion packages to logx and
// drops the rest: their debug and trace output prints ICE credentials
// (ice agent.go "Started agent: … remotePwd") and packet contents.
type pionLogger struct {
	scope string
}

type pionLoggerFactory struct{}

func newLoggerFactory() logging.LoggerFactory { return pionLoggerFactory{} }

func (pionLoggerFactory) NewLogger(scope string) logging.LeveledLogger {
	return pionLogger{scope: scope}
}

func (pionLogger) Trace(string)          {}
func (pionLogger) Tracef(string, ...any) {}
func (pionLogger) Debug(string)          {}
func (pionLogger) Debugf(string, ...any) {}
func (pionLogger) Info(string)           {}
func (pionLogger) Infof(string, ...any)  {}

func (l pionLogger) Warn(msg string) { logx.Infof("sfu: pion %s: %s", l.scope, msg) }

func (l pionLogger) Warnf(format string, args ...any) {
	logx.Infof("sfu: pion %s: %s", l.scope, fmt.Sprintf(format, args...))
}

func (l pionLogger) Error(msg string) { logx.Errorf("sfu: pion %s: %s", l.scope, msg) }

func (l pionLogger) Errorf(format string, args ...any) {
	logx.Errorf("sfu: pion %s: %s", l.scope, fmt.Sprintf(format, args...))
}
