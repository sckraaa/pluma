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

package links

import (
	"fmt"
	"net/http"
	"strings"
)

// The texts of the pages, in the languages of the site (Russian at its
// root, English): those of t.me's pages (docs/tasks/026-research/tme.md)
// for Pluma. {0}, {1} take arguments; a key missing in a language falls
// back to English.
var texts = map[string]map[string]string{
	"en": {
		"lang":              "en",
		"download":          "Download",
		"open_web":          "Open in browser",
		"view_title":        "Pluma: View @{0}",
		"contact_title":     "Pluma: Contact @{0}",
		"launch_title":      "Pluma: Launch @{0}",
		"subscribers":       "{0} subscribers",
		"subscriber":        "{0} subscriber",
		"members":           "{0} members",
		"member":            "{0} member",
		"view_in_app":       "View in Pluma",
		"send_message":      "Send Message",
		"start_bot":         "Start Bot",
		"open_app":          "Open App",
		"view_post":         "View Post",
		"preview_channel":   "Preview channel",
		"view_context":      "View context",
		"if_join":           "If you have <strong>Pluma</strong>, you can view and join<br>{0} right away.",
		"if_contact":        "If you have <strong>Pluma</strong>, you can contact<br>{0} right away.",
		"if_launch":         "If you have <strong>Pluma</strong>, you can launch<br>{0} right away.",
		"if_post":           "If you have <strong>Pluma</strong>, you can view post<br>and join {0} right away.",
		"unknown_desc":      "If you have <strong>Pluma</strong>, you can contact {0} right away.",
		"join_title":        "Pluma: Join Group Chat",
		"join_og":           "Join group chat on Pluma",
		"join_desc":         "You are invited to a <strong>group chat</strong> on <strong>Pluma</strong>. Click to join:",
		"join_button":       "Join Group",
		"phone_title":       "Pluma: Contact",
		"phone_heading":     "Chat with {0}",
		"open_chat":         "Open Chat",
		"private_title":     "Pluma",
		"private_post_desc": "Message in a private group or channel.<br>This link will only work if you are a member.",
		"open_message":      "Open Message",
		"private_boost":     "Boost in a private group or channel.<br>This link will only work if you are a member.",
		"boost_button":      "Boost",
		"stickers_title":    "Pluma: Add Sticker Set",
		"stickers_og":       "Add sticker set {0} on Pluma",
		"stickers_og_none":  "Add sticker set on Pluma",
		"stickers_desc":     "A Pluma user has created the <strong>{0}</strong> sticker set.",
		"stickers_none":     "Sticker Set",
		"stickers_button":   "Add Stickers",
		"emoji_title":       "Pluma: Add Emoji Set",
		"emoji_og":          "Add emoji set {0} on Pluma",
		"emoji_desc":        "A Pluma user has created the <strong>{0}</strong> emoji set.",
		"emoji_button":      "Add Emoji",
		"share_title":       "Pluma: Share Web Page",
		"share_button":      "Share",
		"proxy_title":       "Pluma: Apply MTProxy Settings",
		"proxy_button":      "Apply MTProxy Settings",
		"socks_title":       "Pluma: Apply Proxy Settings",
		"socks_button":      "Apply Proxy Settings",
		"language_title":    "Pluma: Set Language",
		"language_og":       "Pluma Language",
		"language_desc":     "Press the button below to translate the interface of your <strong>Pluma</strong> app to a different language.",
		"language_button":   "Set Language",
		"theme_title":       "Pluma: Apply Theme",
		"theme_og":          "Pluma Theme",
		"theme_desc":        "Press the button below to apply <strong>{0}</strong> theme to your <strong>Pluma</strong> app.",
		"theme_button":      "Apply Theme",
		"bg_title":          "Pluma: Apply Chat Background",
		"bg_button":         "Apply Background",
		"folder_title":      "Pluma: Add Chats",
		"folder_og":         "Pluma Chats",
		"folder_desc":       "You can add new chats to your chat list on <strong>Pluma</strong>.",
		"folder_button":     "View Chats",
		"login_title":       "Pluma: Code",
		"login_desc":        "Your login code is:<br><big><strong>{0}</strong></big><br><br>Enter it in your <strong>Pluma</strong> app or press the button below to authorize this device.",
		"login_button":      "Log in with code",
		"contact_link":      "Expiring link to contact user on <strong>Pluma</strong>.",
		"open_contact":      "Open Contact",
		"chatlink_title":    "Pluma: Chat with {0}",
		"chatlink_none":     "Pluma: Chat",
		"chatlink_desc":     "If you have <strong>Pluma</strong>, you can open this chat right away.",
		"call_title":        "Pluma: Join Call",
		"call_desc":         "You are invited to a call on <strong>Pluma</strong>.",
		"call_button":       "Join Call",

		// The login on a site through oauth.pluma.chat (task 106): the card of
		// the link that opens a request in the app, and the page of the
		// request on the provider's host. Plain texts: the provider's page
		// escapes them.
		"oauth_link_title":        "Pluma: Log In",
		"oauth_link_desc":         "A site asks you to log in with <strong>Pluma</strong>. Open the app to confirm or decline.",
		"oauth_link_button":       "Open in Pluma",
		"oauth_page_title":        "Pluma: Log in to {0}",
		"oauth_heading":           "Log in to {0}",
		"oauth_bot":               "The site’s bot: {0}",
		"oauth_scan":              "Scan the code with the camera of a phone that has Pluma, or open Pluma on this device.",
		"oauth_button":            "Open Pluma",
		"oauth_match":             "In Pluma, choose this emoji:",
		"oauth_waiting":           "Waiting for your answer in Pluma…",
		"oauth_done":              "Done. Returning to the site…",
		"oauth_expired":           "This login request has expired.",
		"oauth_retry":             "Try again",
		"oauth_declined":          "You declined the login in Pluma.",
		"oauth_back":              "Back to the site",
		"oauth_error_title":       "Pluma: Login",
		"oauth_error_heading":     "Cannot log in",
		"oauth_error_client":      "The site is not set up to log in with Pluma, or it sent you to a wrong address.",
		"oauth_error_unavailable": "Logging in to this site with Pluma is unavailable now.",
		"oauth_error_busy":        "Too many login attempts. Try again in a minute.",
		"oauth_error_server":      "Something went wrong. Try again later.",

		"preview_title":     "{0} – Pluma",
		"preview_join":      "Join",
		"preview_search":    "Search",
		"preview_more":      "Show earlier posts",
		"preview_newer":     "Show later posts",
		"preview_empty":     "No posts yet.",
		"preview_photo":     "Photo",
		"preview_video":     "Video",
		"preview_file":      "File",
		"preview_media":     "Media",
		"preview_service":   "Service message",
		"preview_open_post": "Open in Pluma",
		"preview_views":     "{0} views",
		"default_desc":      "Pluma is a messenger that belongs to the people who use it.",
	},
	"ru": {
		"lang":              "ru",
		"download":          "Скачать",
		"open_web":          "Открыть в браузере",
		"view_title":        "Pluma: @{0}",
		"contact_title":     "Pluma: написать @{0}",
		"launch_title":      "Pluma: запустить @{0}",
		"subscribers":       "{0} подписчиков",
		"subscriber":        "{0} подписчик",
		"subscribers_few":   "{0} подписчика",
		"members":           "{0} участников",
		"member":            "{0} участник",
		"members_few":       "{0} участника",
		"view_in_app":       "Открыть в Pluma",
		"send_message":      "Написать",
		"start_bot":         "Запустить бота",
		"open_app":          "Открыть приложение",
		"view_post":         "Открыть пост",
		"preview_channel":   "Посмотреть канал",
		"view_context":      "Посмотреть в канале",
		"if_join":           "Если у вас есть <strong>Pluma</strong>, вы можете открыть<br>{0} и вступить прямо сейчас.",
		"if_contact":        "Если у вас есть <strong>Pluma</strong>, вы можете написать<br>{0} прямо сейчас.",
		"if_launch":         "Если у вас есть <strong>Pluma</strong>, вы можете запустить<br>{0} прямо сейчас.",
		"if_post":           "Если у вас есть <strong>Pluma</strong>, вы можете открыть пост<br>и вступить в {0} прямо сейчас.",
		"unknown_desc":      "Если у вас есть <strong>Pluma</strong>, вы можете написать {0} прямо сейчас.",
		"join_title":        "Pluma: приглашение в чат",
		"join_og":           "Приглашение в чат Pluma",
		"join_desc":         "Вас пригласили в <strong>чат</strong> в <strong>Pluma</strong>. Нажмите, чтобы вступить:",
		"join_button":       "Вступить",
		"phone_title":       "Pluma: написать",
		"phone_heading":     "Чат с {0}",
		"open_chat":         "Открыть чат",
		"private_title":     "Pluma",
		"private_post_desc": "Сообщение в закрытой группе или канале.<br>Ссылка откроется, только если вы в них состоите.",
		"open_message":      "Открыть сообщение",
		"private_boost":     "Буст закрытой группы или канала.<br>Ссылка откроется, только если вы в них состоите.",
		"boost_button":      "Поддержать",
		"stickers_title":    "Pluma: набор стикеров",
		"stickers_og":       "Набор стикеров «{0}» в Pluma",
		"stickers_og_none":  "Набор стикеров в Pluma",
		"stickers_desc":     "Пользователь Pluma создал набор стикеров <strong>{0}</strong>.",
		"stickers_none":     "Набор стикеров",
		"stickers_button":   "Добавить стикеры",
		"emoji_title":       "Pluma: набор эмодзи",
		"emoji_og":          "Набор эмодзи «{0}» в Pluma",
		"emoji_desc":        "Пользователь Pluma создал набор эмодзи <strong>{0}</strong>.",
		"emoji_button":      "Добавить эмодзи",
		"share_title":       "Pluma: поделиться страницей",
		"share_button":      "Поделиться",
		"proxy_title":       "Pluma: настройки MTProxy",
		"proxy_button":      "Применить настройки MTProxy",
		"socks_title":       "Pluma: настройки прокси",
		"socks_button":      "Применить настройки прокси",
		"language_title":    "Pluma: язык интерфейса",
		"language_og":       "Язык Pluma",
		"language_desc":     "Нажмите кнопку ниже, чтобы перевести интерфейс <strong>Pluma</strong> на другой язык.",
		"language_button":   "Сменить язык",
		"theme_title":       "Pluma: тема оформления",
		"theme_og":          "Тема Pluma",
		"theme_desc":        "Нажмите кнопку ниже, чтобы применить тему <strong>{0}</strong> в <strong>Pluma</strong>.",
		"theme_button":      "Применить тему",
		"bg_title":          "Pluma: фон чата",
		"bg_button":         "Применить фон",
		"folder_title":      "Pluma: добавить чаты",
		"folder_og":         "Чаты Pluma",
		"folder_desc":       "Вы можете добавить новые чаты в свой список в <strong>Pluma</strong>.",
		"folder_button":     "Посмотреть чаты",
		"login_title":       "Pluma: код",
		"login_desc":        "Ваш код для входа:<br><big><strong>{0}</strong></big><br><br>Введите его в <strong>Pluma</strong> или нажмите кнопку ниже, чтобы войти на этом устройстве.",
		"login_button":      "Войти с кодом",
		"contact_link":      "Временная ссылка, чтобы написать пользователю <strong>Pluma</strong>.",
		"open_contact":      "Открыть контакт",
		"chatlink_title":    "Pluma: чат — {0}",
		"chatlink_none":     "Pluma: чат",
		"chatlink_desc":     "Если у вас есть <strong>Pluma</strong>, вы можете открыть этот чат прямо сейчас.",
		"call_title":        "Pluma: звонок",
		"call_desc":         "Вас приглашают в звонок в <strong>Pluma</strong>.",
		"call_button":       "Присоединиться к звонку",

		"oauth_link_title":        "Pluma: вход",
		"oauth_link_desc":         "Сайт просит войти с помощью <strong>Pluma</strong>. Откройте приложение, чтобы подтвердить или отказаться.",
		"oauth_link_button":       "Открыть в Pluma",
		"oauth_page_title":        "Pluma: вход на {0}",
		"oauth_heading":           "Вход на {0}",
		"oauth_bot":               "Бот сайта: {0}",
		"oauth_scan":              "Отсканируйте код камерой телефона, на котором есть Pluma, или откройте Pluma на этом устройстве.",
		"oauth_button":            "Открыть Pluma",
		"oauth_match":             "В Pluma выберите этот эмодзи:",
		"oauth_waiting":           "Ждём вашего ответа в Pluma…",
		"oauth_done":              "Готово. Возвращаемся на сайт…",
		"oauth_expired":           "Время этого запроса на вход истекло.",
		"oauth_retry":             "Попробовать снова",
		"oauth_declined":          "Вы отказались от входа в Pluma.",
		"oauth_back":              "Вернуться на сайт",
		"oauth_error_title":       "Pluma: вход",
		"oauth_error_heading":     "Войти не получится",
		"oauth_error_client":      "Сайт не настроен для входа через Pluma или отправил вас по неверному адресу.",
		"oauth_error_unavailable": "Вход на этот сайт через Pluma сейчас недоступен.",
		"oauth_error_busy":        "Слишком много попыток входа. Попробуйте через минуту.",
		"oauth_error_server":      "Что-то пошло не так. Попробуйте позже.",

		"preview_title":     "{0} – Pluma",
		"preview_join":      "Вступить",
		"preview_search":    "Поиск",
		"preview_more":      "Показать ранние посты",
		"preview_newer":     "Показать новые посты",
		"preview_empty":     "Постов пока нет.",
		"preview_photo":     "Фото",
		"preview_video":     "Видео",
		"preview_file":      "Файл",
		"preview_media":     "Медиа",
		"preview_service":   "Служебное сообщение",
		"preview_open_post": "Открыть в Pluma",
		"preview_views":     "{0} просмотров",
		"default_desc":      "Pluma — мессенджер, который принадлежит тем, кто им пользуется.",
	},
}

// SiteTexts gives the texts of the site to the pages of other packages:
// the provider of oauth.pluma.chat (task 106) shows its page in the
// language and with the texts of these pages (ADR 0036).
type SiteTexts struct{}

// Lang is the language of the site the visitor of r prefers, en or ru.
func (SiteTexts) Lang(r *http.Request) string { return visitorOf(r).Lang }

// Text is the text of key in lang with args, English for a key lang does
// not have.
func (SiteTexts) Text(lang, key string, args ...string) string { return text(lang, key, args...) }

// text is the text of key in lang with args.
func text(lang, key string, args ...string) string {
	s, ok := texts[lang][key]
	if !ok {
		s = texts["en"][key]
	}
	for i, a := range args {
		s = strings.ReplaceAll(s, fmt.Sprintf("{%d}", i), a)
	}
	return s
}

// count is "N subscribers" or "N members" (key) in lang, in the plural of
// n: Russian has three forms (1 подписчик, 2 подписчика, 5 подписчиков).
func count(lang, key string, n int) string {
	num := groupDigits(n)
	switch {
	case lang == "ru":
		mod10, mod100 := n%10, n%100
		switch {
		case mod10 == 1 && mod100 != 11:
			return text(lang, strings.TrimSuffix(key, "s"), num)
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return text(lang, key+"_few", num)
		}
		return text(lang, key, num)
	case n == 1:
		return text(lang, strings.TrimSuffix(key, "s"), num)
	}
	return text(lang, key, num)
}

// groupDigits writes n with thin groups of three, as t.me does
// ("10 116 580").
func groupDigits(n int) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	return b.String()
}
