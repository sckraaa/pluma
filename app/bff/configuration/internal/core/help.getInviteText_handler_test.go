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

import (
	"context"
	"net/url"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/env2"

	"github.com/zeromicro/go-zero/core/logx"
)

// The invite text is the address of the link host and nothing else:
// Android puts it into its own sentence in place of the link.
func TestHelpGetInviteText(t *testing.T) {
	c := &ConfigurationCore{ctx: context.Background(), Logger: logx.WithContext(context.Background())}
	r, err := c.HelpGetInviteText(&mtproto.TLHelpGetInviteText{})
	if err != nil || r.GetPredicateName() != mtproto.Predicate_help_inviteText {
		t.Fatalf("%v, %v", r, err)
	}
	u, err := url.Parse(r.GetMessage())
	if err != nil || u.Scheme != "https" || u.Host != env2.TDotMe || u.Path != "" || u.RawQuery != "" {
		t.Errorf("the message %q, want the address https://%s alone", r.GetMessage(), env2.TDotMe)
	}
}
