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
	"fmt"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"google.golang.org/protobuf/proto"
)

// Recorder keeps the steps of a scenario run.
type Recorder struct {
	scenario string
	labels   *Labeler
	steps    []Step
	own      *ownScope // see Scope
}

// NewRecorder starts the record of a scenario.
func NewRecorder(scenario string) *Recorder {
	return &Recorder{scenario: scenario, labels: NewLabeler()}
}

// Scope keeps of every answer only what concerns account self in its own
// chat, Saved Messages (ownScope): a real account lives on while a scenario
// runs, and its other chats are not the scenario's.
func (r *Recorder) Scope(self int64) { r.own = newOwnScope(self) }

// Dropped returns what Scope took out of the answers: paths and types.
func (r *Recorder) Dropped() []string {
	if r.own == nil {
		return nil
	}
	return r.own.dropped
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
			t := Tree(m)
			if r.own != nil {
				before := len(r.own.dropped)
				t = r.own.apply(t)
				for i := before; i < len(r.own.dropped); i++ {
					r.own.dropped[i] = fmt.Sprintf("step %d.%s", len(r.steps), r.own.dropped[i])
				}
			}
			step.Result = r.labels.Label(t)
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
