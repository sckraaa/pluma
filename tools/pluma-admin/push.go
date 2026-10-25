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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/teamgram/teamgram-server/pkg/conf"
	"github.com/teamgram/teamgram-server/pkg/pushprovider"
)

// pushConfig is the part of push.yaml that push check reads.
type pushConfig struct {
	APNs pushprovider.APNsConfig
	FCM  pushprovider.FCMConfig
}

// The test tokens: no device has them, so a provider that takes our key
// refuses the token (task 009, docs/ops/push.md).
const (
	checkAPNsToken = "0000000000000000000000000000000000000000000000000000000000000000"
	checkFCMToken  = "pluma-push-check"
)

// pushCommand: push check asks APNs (production and sandbox) and FCM
// (validate_only) whether they take the keys, without delivering anything.
// Run it where the keys are mounted: in the push container.
func pushCommand(args []string, configFile string, stdout io.Writer) error {
	if len(args) != 2 || args[1] != "check" {
		return errUsage
	}
	var c pushConfig
	if err := conf.Load(configFile, &c); err != nil {
		return fmt.Errorf("config %s: %w", configFile, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	failed := false
	report := func(name string, out pushprovider.Outcome, err error) {
		switch out {
		case pushprovider.OutcomeTokenInvalid:
			fmt.Fprintf(stdout, "%s: the key works (the test token was refused, as expected)\n", name)
		case pushprovider.OutcomeSent:
			fmt.Fprintf(stdout, "%s: the key works\n", name)
		case pushprovider.OutcomeDisabled:
			fmt.Fprintf(stdout, "%s: off, no key configured\n", name)
		case pushprovider.OutcomeAuthFailed:
			failed = true
			fmt.Fprintf(stdout, "%s: the key is refused: %v\n", name, err)
		default:
			failed = true
			fmt.Fprintf(stdout, "%s: %s: %v\n", name, out, err)
		}
	}

	apns, err := pushprovider.NewAPNs(c.APNs, nil)
	switch {
	case err != nil:
		failed = true
		fmt.Fprintf(stdout, "APNs: %v\n", err)
	case apns == nil:
		report("APNs", pushprovider.OutcomeDisabled, nil)
	default:
		body := []byte(`{"aps":{"alert":"pluma push check"}}`)
		for _, gw := range []struct {
			name    string
			sandbox bool
		}{{"APNs production", false}, {"APNs sandbox", true}} {
			out, err := apns.Send(ctx, &pushprovider.APNsPush{Token: checkAPNsToken, Sandbox: gw.sandbox, Type: pushprovider.APNsAlert, Body: body})
			report(gw.name+" (topic "+c.APNs.Topic+")", out, err)
		}
	}

	fcm, err := pushprovider.NewFCM(c.FCM, nil)
	switch {
	case err != nil:
		failed = true
		fmt.Fprintf(stdout, "FCM: %v\n", err)
	case fcm == nil:
		report("FCM", pushprovider.OutcomeDisabled, nil)
	default:
		out, err := fcm.Send(ctx, checkFCMToken, map[string]string{"p": "check"}, true)
		report("FCM (project "+fcm.ProjectId()+")", out, err)
	}
	if failed {
		return errors.New("push check: a provider does not take its key")
	}
	return nil
}
