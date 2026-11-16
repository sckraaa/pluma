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

package translate

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// The profile of HY-MT1.5 (Tencent, huggingface.co/tencent/HY-MT1.5-1.8B):
// its prompts, sampling and languages are those of the model card at
// revision dbad03788f49709801014c95d481a514c272ca52 (README.md, "Prompts"
// and "Supported languages").
const (
	hymtName    = "hy-mt1.5"
	hymtVersion = 1
)

// The prompts of the model card, with its own placeholders.
const (
	// "Prompt Template for XX<=>XX Translation, excluding ZH<=>XX".
	hymtPromptXX = "Translate the following segment into {target_language}, without additional explanation.\n\n{source_text}"
	// "Prompt Template for ZH<=>XX Translation".
	hymtPromptZH = "将以下文本翻译为{target_language}，注意只需要输出翻译后的结果，不要额外解释：\n\n{source_text}"
	// "Prompt Template for formatted translation". The card shows it with
	// 中文 (into Chinese) only; {target_language} stands where 中文 stood
	// and takes the Chinese name of the language, as in the ZH<=>XX
	// prompt. Whether the model keeps the tags into other languages as
	// well is measured before the translation is turned on (brief 101,
	// part 1).
	hymtPromptTagged = "将以下<source></source>之间的文本翻译为{target_language}，注意只需要输出翻译后的结果，不要额外解释，原文中的<sn></sn>标签表示标签内文本包含格式信息，需要在译文中相应的位置尽量保留该标签。输出格式为：<target>str</target>\n\n<source>{source_text}</source>"

	hymtTargetOpen  = "<target>"
	hymtTargetClose = "</target>"
)

// The sampling the model card recommends ("We recommend using the
// following set of parameters for inference").
const (
	hymtTemperature   = 0.7
	hymtTopP          = 0.6
	hymtTopK          = 20
	hymtRepeatPenalty = 1.05
)

// hymtLang is a language of the model: its code in the card and its names
// for the English and the Chinese prompts.
type hymtLang struct {
	code, english, chinese string
}

// hymtLangs is the table "Supported languages" of the card, in its order:
// 33 languages and 5 variants.
var hymtLangs = []hymtLang{
	{"zh", "Chinese", "中文"},
	{"en", "English", "英语"},
	{"fr", "French", "法语"},
	{"pt", "Portuguese", "葡萄牙语"},
	{"es", "Spanish", "西班牙语"},
	{"ja", "Japanese", "日语"},
	{"tr", "Turkish", "土耳其语"},
	{"ru", "Russian", "俄语"},
	{"ar", "Arabic", "阿拉伯语"},
	{"ko", "Korean", "韩语"},
	{"th", "Thai", "泰语"},
	{"it", "Italian", "意大利语"},
	{"de", "German", "德语"},
	{"vi", "Vietnamese", "越南语"},
	{"ms", "Malay", "马来语"},
	{"id", "Indonesian", "印尼语"},
	{"tl", "Filipino", "菲律宾语"},
	{"hi", "Hindi", "印地语"},
	{"zh-Hant", "Traditional Chinese", "繁体中文"},
	{"pl", "Polish", "波兰语"},
	{"cs", "Czech", "捷克语"},
	{"nl", "Dutch", "荷兰语"},
	{"km", "Khmer", "高棉语"},
	{"my", "Burmese", "缅甸语"},
	{"fa", "Persian", "波斯语"},
	{"gu", "Gujarati", "古吉拉特语"},
	{"ur", "Urdu", "乌尔都语"},
	{"te", "Telugu", "泰卢固语"},
	{"mr", "Marathi", "马拉地语"},
	{"he", "Hebrew", "希伯来语"},
	{"bn", "Bengali", "孟加拉语"},
	{"ta", "Tamil", "泰米尔语"},
	{"uk", "Ukrainian", "乌克兰语"},
	{"bo", "Tibetan", "藏语"},
	{"kk", "Kazakh", "哈萨克语"},
	{"mn", "Mongolian", "蒙古语"},
	{"ug", "Uyghur", "维吾尔语"},
	{"yue", "Cantonese", "粤语"},
}

// hymtAliases are other spellings of languages of the table: the codes
// Java's Locale gives for Hebrew and Indonesian, and the ISO 639-2 code of
// Filipino, which a locale of the phone may carry.
var hymtAliases = map[string]string{
	"iw":  "he",
	"in":  "id",
	"fil": "tl",
}

// hymtChinese are the targets the Chinese prompt is for ("ZH<=>XX"); the
// card names the English one for every other pair.
var hymtChinese = map[string]bool{"zh": true, "zh-Hant": true, "yue": true}

var errNoTarget = errors.New("translate: the answer is not in <target>")

type hymt struct {
	slotContext int
	langs       map[string]hymtLang
}

func newHYMT(slotContext int) Profile {
	p := &hymt{slotContext: slotContext, langs: make(map[string]hymtLang, len(hymtLangs))}
	for _, l := range hymtLangs {
		p.langs[l.code] = l
	}
	return p
}

func (p *hymt) Name() string { return hymtName }
func (p *hymt) Version() int { return hymtVersion }
func (p *hymt) ID() string   { return profileID(hymtName, hymtVersion) }

// Lang takes xx, xx-YY and xx_YY in any case (ADR 0064, section 4): Android
// sends "xx" or "xx-YY" (TranslateController.java:2123-2133), tdesktop a
// two-letter code (lang/translate_mtproto_provider.cpp:107). Chinese goes
// by its script, then by its region: zh-Hans, zh-CN, zh-SG are zh; zh-Hant,
// zh-TW, zh-HK, zh-MO are zh-Hant. Any other region is dropped (pt-BR is
// pt). Norwegian ("no", which Android sends for "nb",
// TranslateAlert2.java:336-338) is not a language of the model.
func (p *hymt) Lang(toLang string) (string, error) {
	s := strings.ToLower(strings.ReplaceAll(toLang, "_", "-"))
	if s == "" || len(s) > 16 {
		return "", ErrToLangInvalid
	}
	parts := strings.Split(s, "-")
	for _, part := range parts {
		if part == "" {
			return "", ErrToLangInvalid
		}
		for _, c := range part {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return "", ErrToLangInvalid
			}
		}
	}
	primary := parts[0]
	if alias, ok := hymtAliases[primary]; ok {
		primary = alias
	}
	if primary == "zh" {
		traditional := false
		for _, sub := range parts[1:] {
			switch sub {
			case "hans":
				return "zh", nil
			case "hant", "tw", "hk", "mo":
				traditional = true
			}
		}
		if traditional {
			return "zh-Hant", nil
		}
		return "zh", nil
	}
	if l, ok := p.langs[primary]; ok {
		return l.code, nil
	}
	return "", ErrToLangInvalid
}

func (p *hymt) Request(lang, text string, tagged bool) (Request, error) {
	l, ok := p.langs[lang]
	if !ok {
		return Request{}, ErrToLangInvalid
	}
	var prompt string
	switch {
	case tagged:
		prompt = strings.NewReplacer("{target_language}", l.chinese, "{source_text}", text).Replace(hymtPromptTagged)
	case hymtChinese[lang]:
		prompt = strings.NewReplacer("{target_language}", l.chinese, "{source_text}", text).Replace(hymtPromptZH)
	default:
		prompt = strings.NewReplacer("{target_language}", l.english, "{source_text}", text).Replace(hymtPromptXX)
	}
	chars := utf8.RuneCountInString(text)
	req := Request{
		Prompt:        prompt,
		Temperature:   hymtTemperature,
		TopP:          hymtTopP,
		TopK:          hymtTopK,
		RepeatPenalty: hymtRepeatPenalty,
		MaxTokens:     p.maxTokens(chars),
		Chars:         chars,
	}
	if tagged {
		// Nothing after the translation is of use. Answer reads the
		// translation with and without the closing tag, whether the
		// engine leaves the stop word in the content or not.
		req.Stop = []string{hymtTargetClose}
	}
	return req, nil
}

// maxTokens bounds the answer to chars characters of source (brief 101):
// 3/2 of a token a character and 64 for the tags, at most half the context
// of a slot, so that the prompt keeps the other half.
func (p *hymt) maxTokens(chars int) int {
	return min(3*chars/2+64, p.slotContext/2)
}

func (p *hymt) Answer(answer string, tagged bool) (string, error) {
	if !tagged {
		return strings.TrimSpace(answer), nil
	}
	i := strings.Index(answer, hymtTargetOpen)
	if i < 0 {
		return "", errNoTarget
	}
	answer = answer[i+len(hymtTargetOpen):]
	if j := strings.Index(answer, hymtTargetClose); j >= 0 {
		answer = answer[:j]
	}
	return answer, nil
}
