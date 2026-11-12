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

package transcription_helper

import (
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/core"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/server/grpc/service"
	"github.com/teamgram/teamgram-server/app/bff/transcription/internal/svc"
)

type (
	Config = config.Config
	// Worker transcribes the queued records and sends the updates owed.
	Worker = core.Worker
)

// New returns messages.transcribeAudio and messages.rateTranscribedAudio
// (task 090, ADR 0056) and starts their worker; Stop of the worker waits
// for it.
func New(c Config) (*service.Service, *Worker) {
	ctx := svc.NewServiceContext(c)
	return service.New(ctx), core.StartWorker(ctx)
}
