// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package user

import (
	"strings"
)

const (
	MinUsernameLen = 5
	MaxUsernameLen = 32
)

const (
	UsernameNotExisted   = 0
	UsernameExisted      = 1
	UsernameExistedNotMe = 2
	UsernameExistedIsMe  = 3
)

// reservedUsernames are the first path segments of the link host
// (pluma.chat/<username>, task 026) that are not usernames: the pages of
// the site and the link paths of core.telegram.org/api/links that are long
// enough to be usernames. A username equal to one of them would never open.
var reservedUsernames = map[string]bool{
	// The site (teamgramd/deploy/web/Caddyfile).
	"android": true, "iphone": true, "windows": true, "linux": true,
	"assets": true, "collect": true,
	// Links (core.telegram.org/api/links, the reserved list of <name>.t.me).
	"addemoji": true, "addlist": true, "addstickers": true, "addstyle": true,
	"addtheme": true, "auction": true, "boost": true, "confirmphone": true,
	"contact": true, "giftcode": true, "invoice": true, "joinchat": true,
	"login": true, "proxy": true, "setlanguage": true, "share": true,
	"socks": true, "newbot": true,
}

// ReservedUsername tells whether username, in any case, is a path of the
// link host rather than a username: USERNAME_INVALID.
func ReservedUsername(username string) bool {
	return reservedUsernames[strings.ToLower(username)]
}

/*
   if (name != null) {
       if (name.startsWith("_") || name.endsWith("_")) {
           checkTextView.setText(LocaleController.getString("LinkInvalid", R.string.LinkInvalid));
           checkTextView.setTag(Theme.key_windowBackgroundWhiteRedText4);
           checkTextView.setTextColor(Theme.getColor(Theme.key_windowBackgroundWhiteRedText4));
           return false;
       }
       for (int a = 0; a < name.length(); a++) {
           char ch = name.charAt(a);
           if (a == 0 && ch >= '0' && ch <= '9') {
               checkTextView.setText(LocaleController.getString("LinkInvalidStartNumber", R.string.LinkInvalidStartNumber));
               checkTextView.setTag(Theme.key_windowBackgroundWhiteRedText4);
               checkTextView.setTextColor(Theme.getColor(Theme.key_windowBackgroundWhiteRedText4));
               return false;
           }
           if (!(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_')) {
               checkTextView.setText(LocaleController.getString("LinkInvalid", R.string.LinkInvalid));
               checkTextView.setTag(Theme.key_windowBackgroundWhiteRedText4);
               checkTextView.setTextColor(Theme.getColor(Theme.key_windowBackgroundWhiteRedText4));
               return false;
           }
       }
   }
   if (name == null || name.length() < 5) {
       checkTextView.setText(LocaleController.getString("LinkInvalidShort", R.string.LinkInvalidShort));
       checkTextView.setTag(Theme.key_windowBackgroundWhiteRedText4);
       checkTextView.setTextColor(Theme.getColor(Theme.key_windowBackgroundWhiteRedText4));
       return false;
   }
   if (name.length() > 32) {
       checkTextView.setText(LocaleController.getString("LinkInvalidLong", R.string.LinkInvalidLong));
       checkTextView.setTag(Theme.key_windowBackgroundWhiteRedText4);
       checkTextView.setTextColor(Theme.getColor(Theme.key_windowBackgroundWhiteRedText4));
       return false;
   }
*/

func CheckUsernameInvalid(username string) bool {
	if len(username) < MinUsernameLen || len(username) > MaxUsernameLen {
		return false
	}

	if strings.HasPrefix(username, "_") || strings.HasSuffix(username, "_") {
		return false
	}

	if username[0] >= '0' && username[0] <= '9' {
		return false
	}

	for _, ch := range username {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_') {
			return false
		}
	}
	return true
}
