package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The public progress counts implemented and partly implemented methods,
// overall and by area. A method of several versions counts once, with the
// best status of them; the methods of messages.* that serve stickers,
// folders or bots count under those; a namespace of no area under "other".
func TestRenderProgress(t *testing.T) {
	ms := []*method{
		{TLName: "messages.getHistory", NS: "messages", Status: "ok"},
		{TLName: "messages.sendMessage", NS: "messages", Status: "partial"},
		{TLName: "messages.sendScreenshotNotification", NS: "messages", Status: "missing"},
		{TLName: "messages.getStickerSet", NS: "messages", Status: "ok"},
		{TLName: "messages.getCustomEmojiDocuments", NS: "messages", Status: "ok"},
		{TLName: "stickers.createStickerSet", NS: "stickers", Status: "missing"},
		{TLName: "messages.getDialogFiltersEFD48C89", NS: "messages", Status: "missing"},
		{TLName: "messages.getDialogFiltersF19ED96D", NS: "messages", Status: "ok"},
		{TLName: "messages.getInlineBotResults", NS: "messages", Status: "missing"},
		{TLName: "users.getUsers", NS: "users", Status: "ok"},
		{TLName: "contacts.block", NS: "contacts", Status: "blocked"},
		{TLName: "smsjobs.join", NS: "smsjobs", Status: "missing"},
	}
	b, err := renderProgress(ms, "228", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Layer   string `json:"layer"`
		Total   int    `json:"total"`
		Ok      int    `json:"ok"`
		Partial int    `json:"partial"`
		Areas   []struct {
			Key   string `json:"key"`
			Total int    `json:"total"`
			Ok    int    `json:"ok"`
		} `json:"areas"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	// 12 entries, two of them one method.
	if got.Layer != "228" || got.Total != 11 || got.Ok != 5 || got.Partial != 1 {
		t.Errorf("overall %+v", got)
	}
	want := map[string][2]int{ // total, ok
		"messages": {3, 1},
		"contacts": {2, 1},
		"stickers": {3, 2},
		"folders":  {1, 1},
		"bots":     {1, 0},
		"other":    {1, 0},
	}
	if len(got.Areas) != len(want) {
		t.Fatalf("areas %+v, want %v", got.Areas, want)
	}
	for _, a := range got.Areas {
		if w, ok := want[a.Key]; !ok || [2]int{a.Total, a.Ok} != w {
			t.Errorf("area %s: %d of %d, want %v", a.Key, a.Ok, a.Total, w)
		}
	}
	if got.Areas[len(got.Areas)-1].Key != "other" {
		t.Errorf("other is not last: %+v", got.Areas)
	}
}

// What the registry decided is not needed leaves the count, unless it is done.
func TestProgressLeavesOutNotNeeded(t *testing.T) {
	ms := []*method{
		{TLName: "payments.getForm", NS: "payments", Status: "missing"},
		{TLName: "payments.getDone", NS: "payments", Status: "ok"},
		{TLName: "messages.getOnlines", NS: "messages", Status: "missing"},
	}
	b, err := renderProgress(ms, "233", map[string]bool{"payments.getForm": true, "payments.getDone": true})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Total int `json:"total"`
		Ok    int `json:"ok"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 2 || got.Ok != 1 {
		t.Errorf("total %d, ok %d; want 2 and 1", got.Total, got.Ok)
	}
}

// Payments and Telegram Business count under "other", and group calls apart
// from calls one to one.
func TestProgressAreas(t *testing.T) {
	for tl, want := range map[string]string{
		"payments.getStarsStatus":         "other",
		"account.updateBusinessWorkHours": "other",
		"account.getConnectedBots":        "other",
		"messages.getQuickReplies":        "other",
		"phone.requestCall":               "calls",
		"phone.getCallConfig":             "calls",
		"phone.joinGroupCall":             "groupcalls",
		"phone.joinGroupCallPresentation": "groupcalls",
		"phone.createConferenceCall":      "groupcalls",
		"phone.getGroupParticipants":      "groupcalls",
		"phone.saveDefaultSendAs":         "groupcalls",
		"account.updateProfile":           "account",
		"messages.sendMessage":            "messages",
	} {
		ns, _, _ := strings.Cut(tl, ".")
		if got := areaOf(tl, ns); got != want {
			t.Errorf("%s: area %q, want %q", tl, got, want)
		}
	}
}
