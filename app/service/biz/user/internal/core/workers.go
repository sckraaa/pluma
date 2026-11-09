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

package core

// Workers are the background jobs of the user service, which the server
// starts and stops with itself.
type Workers struct {
	SignUps *SignUpWorker
}

// Start starts every worker.
func (w *Workers) Start() {
	w.SignUps.Start()
}

// Stop stops every worker and waits for its work in progress.
func (w *Workers) Stop() {
	w.SignUps.Stop()
}
