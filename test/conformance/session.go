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

package conformance

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/proto"
)

// Recorder keeps the steps of a scenario run.
type Recorder struct {
	scenario string
	labels   *Labeler
	steps    []Step
}

// NewRecorder starts the record of a scenario.
func NewRecorder(scenario string) *Recorder {
	return &Recorder{scenario: scenario, labels: NewLabeler()}
}

// Call invokes req over c and records the step as call (the TL method
// name, e.g. "auth.sendCode"). The result and error go back to the
// scenario as they are.
func (r *Recorder) Call(ctx context.Context, c *mtclient.Client, call string, req mtproto.TLObject) (mtproto.TLObject, error) {
	o, err := c.Invoke(ctx, req)
	step := Step{Call: call}
	var rpcErr *mtclient.RPCError
	switch {
	case errors.As(err, &rpcErr):
		step.Error = NewError(rpcErr.Code, rpcErr.Message)
	case err != nil:
		return o, err // not an answer of the server: the scenario fails
	default:
		if m, ok := o.(proto.Message); ok {
			step.Result = r.labels.Label(Tree(m))
		}
	}
	r.steps = append(r.steps, step)
	return o, err
}

// Fixture returns what was recorded, the values as a fixture read from
// JSON has them.
func (r *Recorder) Fixture(source string) (*Fixture, error) {
	f := &Fixture{Scenario: r.scenario, Source: source, Steps: r.steps}
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	var out Fixture
	err = json.Unmarshal(b, &out)
	return &out, err
}
