package main

import (
	"encoding/json"
	"testing"
)

// The public progress counts implemented and partly implemented methods,
// overall and by area; a namespace of no area counts under "other".
func TestRenderProgress(t *testing.T) {
	ms := []*method{
		{NS: "messages", Status: "ok"},
		{NS: "messages", Status: "partial"},
		{NS: "messages", Status: "missing"},
		{NS: "users", Status: "ok"},
		{NS: "contacts", Status: "blocked"},
		{NS: "smsjobs", Status: "missing"},
	}
	b, err := renderProgress(ms, "228")
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
	if got.Layer != "228" || got.Total != 6 || got.Ok != 2 || got.Partial != 1 {
		t.Errorf("overall %+v", got)
	}
	want := []string{"messages", "contacts", "other"}
	if len(got.Areas) != len(want) {
		t.Fatalf("areas %+v, want %v", got.Areas, want)
	}
	for i, k := range want {
		if got.Areas[i].Key != k {
			t.Errorf("area %d is %s, want %s", i, got.Areas[i].Key, k)
		}
	}
	if got.Areas[1].Total != 2 || got.Areas[1].Ok != 1 {
		t.Errorf("contacts (with users) %+v", got.Areas[1])
	}
}
