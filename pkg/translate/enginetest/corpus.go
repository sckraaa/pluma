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

package enginetest

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
)

// The corpus is our own: messages as people write them in chats, in the
// languages the measurement is about (brief 101, part 1), written for this
// suite. It holds no text of anyone's, so it may be read, logged and kept.

// Sample is one message of the corpus.
type Sample struct {
	// Name says what the sample is for.
	Name string
	// Lang is the language of Text, a code of the profile.
	Lang string
	Text string
	// Entities are those of Text, as a client sends them.
	Entities []*mtproto.MessageEntity
	// Expect are words that must be in a translation into the language
	// of the key, compared without case: names and numbers that any
	// translation keeps, never a choice of words.
	Expect map[string][]string
}

// TextWithEntities is the sample as messages.translateText carries it.
func (s Sample) TextWithEntities() *mtproto.TextWithEntities {
	return &mtproto.TextWithEntities{Text: s.Text, Entities: s.Entities}
}

// Length is the length of the text in characters.
func (s Sample) Length() int { return len([]rune(s.Text)) }

// Pair is a direction of translation.
type Pair struct{ From, To string }

func (p Pair) String() string { return p.From + "→" + p.To }

// Pairs are the directions the measurement is about: Russian with English
// and with Kazakh both ways, Ukrainian into Russian and English into
// Kazakh (brief 101, part 1).
var Pairs = []Pair{
	{"ru", "en"}, {"en", "ru"},
	{"ru", "kk"}, {"kk", "ru"},
	{"uk", "ru"}, {"en", "kk"},
}

// PlainSamples are messages without entities, by language, one of each
// length the measurement takes (brief 101, "Замер"): about 20 characters,
// 60, and 300 (some sixty words); 4096 is built by Long.
var PlainSamples = map[string][]Sample{
	"ru": {
		{Name: "short", Lang: "ru", Text: "Буду через 10 минут!",
			Expect: map[string][]string{"en": {"10"}, "kk": {"10"}}},
		{Name: "line", Lang: "ru", Text: "Купи по дороге хлеб и молоко, а то дома совсем ничего нет."},
		{Name: "medium", Lang: "ru", Text: "Завтра утром мы поедем на поезде в Казань, так что не забудь билеты и паспорт. " +
			"Поезд отходит в 7:40 с Казанского вокзала, лучше быть на месте минут за двадцать. " +
			"Если опоздаешь, звони сразу, я подожду у входа в третий вагон. " +
			"Обратно вернёмся в воскресенье вечером, а в понедельник у тебя уже первая пара.",
			Expect: map[string][]string{"en": {"Kazan", "7:40"}, "kk": {"7:40"}}},
	},
	"en": {
		{Name: "short", Lang: "en", Text: "Running late, sorry!"},
		{Name: "line", Lang: "en", Text: "Can you pick up some bread and milk on your way home?"},
		{Name: "medium", Lang: "en", Text: "Hi everyone, quick update on the move: the van arrives on Saturday at 9 am, " +
			"so please have your boxes labelled and stacked by the door the night before. " +
			"Anna will take the plants in her car, and I will drive the kids to Grandma's after lunch. " +
			"If anything breaks, keep the pieces and take a photo, the insurance needs both.",
			Expect: map[string][]string{"ru": {"9", "Анн"}, "kk": {"9"}}},
	},
	"kk": {
		{Name: "short", Lang: "kk", Text: "Қазір келемін, күте тұр."},
		{Name: "line", Lang: "kk", Text: "Үйге келе жатып нан мен сүт ала келші, үйде ештеңе жоқ."},
		{Name: "medium", Lang: "kk", Text: "Сәлем! Ертең сағат онда Алматыдағы кеңседе жиналыс болады, кешікпей келуге тырыс. " +
			"Жиналыстан кейін бәріміз бірге түскі асқа барамыз, мейрамхананы Айгүл таңдады. " +
			"Егер ауырып қалсаң немесе кептеліске тап болсаң, маған бірден хабарлас, мен басшыға айтып қоямын. " +
			"Жобаның есебін жұмаға дейін дайындау керек, сондықтан бүгін кешке соңғы нұсқасын жібер.",
			Expect: map[string][]string{"ru": {"Алмат"}}},
	},
	"uk": {
		{Name: "short", Lang: "uk", Text: "Дякую, все отримав!"},
		{Name: "line", Lang: "uk", Text: "Купи дорогою хліб і молоко, бо вдома зовсім нічого немає."},
		{Name: "medium", Lang: "uk", Text: "Привіт! Нагадую, що в суботу ми їдемо до Львова на весілля Олени й Тараса. " +
			"Збираємося о восьмій ранку біля нашого під'їзду, дорога займе приблизно шість годин. " +
			"Візьми з собою теплу куртку, бо ввечері там обіцяють дощ і холодний вітер. " +
			"Подарунок я вже купила, тож тобі залишається тільки підписати листівку від нас обох.",
			Expect: map[string][]string{"ru": {"Льв"}}},
	},
}

// longParagraphs are distinct paragraphs a long message is made of: a
// model that repeats itself is not mistaken for one that translates.
var longParagraphs = map[string][]string{
	"ru": {
		"Коллеги, подвожу итоги квартала. Мы закрыли сорок две задачи из пятидесяти запланированных, и это лучший результат за год. " +
			"Особенно хочу отметить команду поддержки: время первого ответа клиенту сократилось с шести часов до двух, а число повторных обращений упало почти вдвое. " +
			"Отдельное спасибо Марине, которая взяла на себя переезд базы знаний и сделала его без единой минуты простоя.",
		"Теперь о том, что не получилось. Мобильное приложение мы так и не выпустили: проверка в магазине приложений заняла больше времени, чем мы рассчитывали, " +
			"а потом нашлась ошибка в оплате, которую пришлось исправлять уже после отправки. Вывод простой — закладывать на проверку не три дня, а две недели, " +
			"и начинать тестирование оплаты раньше, чем дизайн экранов будет окончательно утверждён.",
		"На следующий квартал у нас три главные цели. Первая — выпустить приложение и довести число активных пользователей до десяти тысяч. " +
			"Вторая — перевести всю отчётность на новую систему, чтобы руководители отделов видели цифры каждый день, а не раз в месяц. " +
			"Третья — нанять двух инженеров и одного дизайнера; описания вакансий уже готовы, собеседования начнём со следующей недели.",
		"Пара организационных моментов. С понедельника офис работает с девяти до восьми, по пятницам — до шести. " +
			"Парковку у главного входа на месяц закроют на ремонт, поэтому машины лучше оставлять во дворе со стороны склада. " +
			"Если кто-то планирует отпуск в декабре, напишите мне до конца месяца, чтобы мы успели распределить дежурства.",
		"И последнее. В пятницу в семь вечера отмечаем окончание квартала в кафе на набережной, приглашены все, включая стажёров. " +
			"Пожалуйста, отметьтесь в таблице, придёте ли вы и будете ли с кем-то, чтобы мы заказали столы на правильное число людей. " +
			"Спасибо всем за работу, без вас этих результатов не было бы.",
	},
	"en": {
		"Hi team, here is the summary of the quarter. We closed forty-two of the fifty planned tasks, the best result this year. " +
			"I especially want to thank the support team: the time to the first reply went down from six hours to two, and repeat requests fell by almost half. " +
			"Special thanks to Marina, who moved the knowledge base without a single minute of downtime.",
		"Now for what did not work. We still have not released the mobile app: the store review took longer than we planned, " +
			"and then we found a payment bug that had to be fixed after submission. The lesson is simple: plan two weeks for the review instead of three days, " +
			"and start testing payments before the screen designs are final.",
		"For the next quarter we have three main goals. The first is to release the app and reach ten thousand active users. " +
			"The second is to move all reporting to the new system, so that heads of departments see the numbers every day instead of once a month. " +
			"The third is to hire two engineers and a designer; the job descriptions are ready and interviews start next week.",
		"A couple of practical things. From Monday the office is open from nine to eight, and until six on Fridays. " +
			"The parking by the main entrance will be closed for repairs for a month, so please leave your cars in the yard behind the warehouse. " +
			"If you plan a holiday in December, write to me by the end of the month so that we can plan the shifts.",
		"And finally, on Friday at seven we celebrate the end of the quarter at the cafe on the embankment, and everyone is invited, interns too. " +
			"Please mark in the spreadsheet whether you are coming and whether you bring someone, so that we book tables for the right number of people. " +
			"Thank you all for your work, these results would not exist without you.",
	},
}

// Long returns a message in lang of about chars characters, at most chars,
// made of whole paragraphs of the corpus over and over; ok is false for a
// language without them.
func Long(lang string, chars int) (Sample, bool) {
	paras := longParagraphs[lang]
	if len(paras) == 0 {
		return Sample{}, false
	}
	var b strings.Builder
	for i := 0; ; i++ {
		p := paras[i%len(paras)]
		sep := ""
		if b.Len() > 0 {
			sep = "\n\n"
		}
		if len([]rune(b.String()))+len([]rune(sep+p)) > chars {
			break
		}
		b.WriteString(sep + p)
	}
	return Sample{Name: fmt.Sprintf("long-%d", chars), Lang: lang, Text: b.String()}, true
}

// TaggedSamples are messages with entities of every kind the markup knows
// (ADR 0064, section 3): formatting inside a link, overlapping formatting,
// verbatim entities (code, mentions, hashtags, a custom emoji, links,
// addresses), a quote and a block of code.
var TaggedSamples = []Sample{
	tagged("bold inside a link", "ru", "Подробности в нашей новой статье на сайте, почитай до вечера.",
		at(mtproto.Predicate_messageEntityTextUrl, "нашей новой статье", withURL("https://example.org/news/42")),
		at(mtproto.Predicate_messageEntityBold, "новой")),
	tagged("mention and hashtag", "ru", "Спроси у @ivan_petrov про отчёт и поставь тег #отчёт2026, пожалуйста.",
		at(mtproto.Predicate_messageEntityMention, "@ivan_petrov"),
		at(mtproto.Predicate_messageEntityHashtag, "#отчёт2026")),
	tagged("code", "ru", "Запусти make build ещё раз и пришли мне весь лог целиком.",
		at(mtproto.Predicate_messageEntityCode, "make build")),
	tagged("custom emoji", "ru", "Отличная работа 🔥 всем спасибо, выпуск в пятницу!",
		at(mtproto.Predicate_messageEntityCustomEmoji, "🔥", withDocument(5368324170671202286)),
		at(mtproto.Predicate_messageEntityBold, "выпуск в пятницу")),
	tagged("quote", "ru", "Он написал: я приеду в пятницу вечером, встречайте на вокзале. Ждём!",
		at(mtproto.Predicate_messageEntityBlockquote, "я приеду в пятницу вечером, встречайте на вокзале.")),
	tagged("overlapping formatting", "ru", "Важно: не опаздывайте на встречу, начнём ровно в десять.",
		at(mtproto.Predicate_messageEntityBold, "Важно: не"),
		at(mtproto.Predicate_messageEntityItalic, "не опаздывайте на встречу")),
	tagged("addresses", "ru", "Пиши на support@example.org или звони +7 900 123-45-67, инструкция на https://example.org/help",
		at(mtproto.Predicate_messageEntityEmail, "support@example.org"),
		at(mtproto.Predicate_messageEntityPhone, "+7 900 123-45-67"),
		at(mtproto.Predicate_messageEntityUrl, "https://example.org/help")),
	tagged("block of code", "ru", "Вот так выводится строка:\nfmt.Println(\"привет\")\nПопробуй сам.",
		at(mtproto.Predicate_messageEntityPre, "fmt.Println(\"привет\")", withLanguage("go"))),
	tagged("spoiler, strike, underline", "ru", "Концовка фильма: герой выживает. Билеты стоили 500 рублей, теперь 700.",
		at(mtproto.Predicate_messageEntitySpoiler, "герой выживает"),
		at(mtproto.Predicate_messageEntityStrike, "500 рублей"),
		at(mtproto.Predicate_messageEntityUnderline, "теперь 700")),
	tagged("formatting and code", "en", "Meet me at the main entrance at nine, the door code is 4512.",
		at(mtproto.Predicate_messageEntityBold, "main entrance"),
		at(mtproto.Predicate_messageEntityItalic, "nine"),
		at(mtproto.Predicate_messageEntityCode, "4512")),
	tagged("link and mention", "en", "Thanks @anna_k for the slides, the recording is on our channel now.",
		at(mtproto.Predicate_messageEntityMention, "@anna_k"),
		at(mtproto.Predicate_messageEntityTextUrl, "our channel", withURL("https://example.org/c/7"))),
	tagged("bold and mention", "kk", "Ертең сағат тоғызда кездесеміз, @aidos_k да келеді.",
		at(mtproto.Predicate_messageEntityBold, "сағат тоғызда"),
		at(mtproto.Predicate_messageEntityMention, "@aidos_k")),
	tagged("formatting and link", "uk", "Нагадую: збори перенесли на четвер, деталі за посиланням https://example.org/u",
		at(mtproto.Predicate_messageEntityBold, "збори"),
		at(mtproto.Predicate_messageEntityItalic, "четвер"),
		at(mtproto.Predicate_messageEntityUrl, "https://example.org/u")),
}

// TaggedTargets are the languages each tagged sample is translated into,
// by its language.
var TaggedTargets = map[string][]string{
	"ru": {"en", "kk"},
	"en": {"ru", "kk"},
	"kk": {"ru"},
	"uk": {"ru"},
}

func tagged(name, lang, text string, ents ...func(string) *mtproto.MessageEntity) Sample {
	s := Sample{Name: name, Lang: lang, Text: text}
	for _, e := range ents {
		s.Entities = append(s.Entities, e(text))
	}
	return s
}

// at makes an entity of kind pred over the first sub of the text it is
// given; offsets and lengths are UTF-16 code units, as MTProto counts them.
// A sub not in the text is a mistake of the corpus and panics.
func at(pred, sub string, set ...func(*mtproto.MessageEntity)) func(string) *mtproto.MessageEntity {
	return func(text string) *mtproto.MessageEntity {
		i := strings.Index(text, sub)
		if i < 0 {
			panic(fmt.Sprintf("enginetest: %q is not in %q", sub, text))
		}
		e := &mtproto.MessageEntity{PredicateName: pred, Offset: units(text[:i]), Length: units(sub)}
		for _, f := range set {
			f(e)
		}
		return e
	}
}

func withURL(u string) func(*mtproto.MessageEntity) {
	return func(e *mtproto.MessageEntity) { e.Url = u }
}

func withLanguage(l string) func(*mtproto.MessageEntity) {
	return func(e *mtproto.MessageEntity) { e.Language = l }
}

func withDocument(id int64) func(*mtproto.MessageEntity) {
	return func(e *mtproto.MessageEntity) { e.DocumentId = id }
}

func units(s string) int32 { return int32(len(utf16.Encode([]rune(s)))) }

// covered returns the text an entity covers.
func covered(text string, e *mtproto.MessageEntity) string {
	u := utf16.Encode([]rune(text))
	s, n := int(e.GetOffset()), int(e.GetLength())
	if s < 0 || n < 0 || s+n > len(u) {
		return ""
	}
	return string(utf16.Decode(u[s : s+n]))
}
