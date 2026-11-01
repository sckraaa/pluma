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

package langpack

import "testing"

// Sentences of Telegram's packs (2026-11-01), shortened.
func TestInflect(t *testing.T) {
	for _, c := range []struct {
		lang, in, want string
	}{
		{"cs", "Vítejte v Telegramu", "Vítejte v Plumě"},
		{"cs", "Přihlásit se na **Telegramu**", "Přihlásit se na **Plumě**"},
		{"cs", "Stáhněte si verzi Telegramu do počítače", "Stáhněte si verzi Plumy do počítače"},
		{"cs", "Povolte Telegramu přístup ke kontaktům", "Povolte Plumě přístup ke kontaktům"},
		{"cs", "Přihlásit se Telegramem", "Přihlásit se Plumou"},
		{"cs", "Ve verzi, Telegramu", "Ve verzi, Plumy"},
		{"sk", "Pozvite priateľov do Telegramu", "Pozvite priateľov do Plumy"},
		{"sk", "Povoľte Telegramu prístup", "Povoľte Plume prístup"},
		{"sk", "Vitajte v Telegrame. Prihláste sa Telegramom", "Vitajte v Plume. Prihláste sa Plumou"},
		{"sk", "Telegramové hviezdy", "Plumové hviezdy"},
		{"pl", "Wersja Telegrama w Telegramie, dzięki Telegramowi, z Telegramem",
			"Wersja Plumy w Plumie, dzięki Plumie, z Plumą"},
		{"hr", "Dobrodošli u Telegram! Verzija Telegrama u Telegramu s Telegramom",
			"Dobrodošli u Pluma! Verzija Plume u Plumi s Plumom"},
		{"hr", "Telegramove zvjezdice, Telegramovih korisnika", "Plumine zvjezdice, Pluminih korisnika"},
		{"sr", "Добродошли у Telegram-у, верзија Telegram-а, Telegram-ови сервери",
			"Добродошли у Plumi, верзија Plume, Plumini сервери"},
		{"fi", "Telegramin käyttäjät Telegramissa, Telegramiin, Telegramia, Telegram-äänipuhelu",
			"Pluman käyttäjät Plumassa, Plumaan, Plumaa, Pluma-äänipuhelu"},
		{"hu", "A Telegramon, a Telegramnak, a Telegramot, Telegrammal, Telegram-fiók",
			"A Plumán, a Plumának, a Plumát, Plumával, Pluma-fiók"},
		{"uz", "Telegramga xush kelibsiz, Telegramdagi, Telegramingiz", "Plumaga xush kelibsiz, Plumadagi, Plumangiz"},
		{"kk", "Telegram-ға қош келдіңіз, Telegram-ды, Telegram-нан, Telegram-ы",
			"Pluma-ға қош келдіңіз, Pluma-ны, Pluma-дан, Pluma-сы"},
		{"tr", "Telegram'ın, Telegram'a, Telegram’ı, Telegram'da, Telegramı", "Pluma'nın, Pluma'ya, Pluma’yı, Pluma'da, Pluma'yı"},
		{"sv", "Telegrams Telegramkonto och Telegram-konto", "Plumas Pluma-konto och Pluma-konto"},
		{"nb", "Telegramskyen", "Pluma-skyen"},
		{"de", "Telegramnutzer, Telegramsuche, Telegrams, Telegramm, Telegramme", "Pluma-Nutzer, Pluma-Suche, Plumas, Telegramm, Telegramme"},
		{"ms", "Telegramnya", "Plumanya"},
		{"sr", "Контакт се придружио Телеграму. Телеграм", "Контакт се придружио Plumi. Pluma"},
		{"kk", "Телеграмды жаңарту", "Pluma-ны жаңарту"},
		// Capitals, and the lower case of compounds where a language writes
		// it.
		{"pl", "WERSJA TELEGRAMA", "WERSJA PLUMY"},
		{"cs", "V TELEGRAMU", "V PLUMĚ"},
		{"tr", "TELEGRAMDAKİ, TELEGRAM'IN", "PLUMA'DAKİ, PLUMA'NIN"},
		{"fi", "TELEGRAMISSA", "PLUMASSA"},
		{"sv", "ditt telegramkonto, telegrammoln, telegrammet", "ditt Pluma-konto, Pluma-moln, telegrammet"},
		{"sk", "telegramový účet", "Plumový účet"},
		{"cs", "telegramu", "telegramu"},
		{"pl", "TELEGRAMa", "TELEGRAMa"},
		// Not inflections: handles, addresses, unknown endings, other
		// languages.
		{"pl", "t.me/TelegramTipsPL @TelegramTips", "t.me/TelegramTipsPL @TelegramTips"},
		{"cs", "Telegramovi", "Telegramovi"},
		{"en", "Telegramu", "Telegramu"},
		{"pt-br", "Telegrams", "Telegrams"},
	} {
		if got := RebrandValue(c.lang, "k", c.in); got != c.want {
			t.Errorf("%s: %q\n got %q\nwant %q", c.lang, c.in, got, c.want)
		}
	}
}
