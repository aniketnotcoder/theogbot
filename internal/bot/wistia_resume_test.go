package bot

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/aniketnotcoder/theogbot/internal/forward"
)

func TestParseWistiaResumeCommandUsesFailedPostIDAsStart(t *testing.T) {
	link, start, end, err := parseWistiaResumeCommand([]string{
		"/wistia", "resume", "https://t.me/c/4451402994/4/82", "to", "157",
	})
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://t.me/c/4451402994/4/82" || start != 82 || end != 157 {
		t.Fatalf("got (%q, %d, %d), want topic 4 post 82 through 157", link, start, end)
	}
}

func TestParseWistiaResumeCommandRejectsInvalidRanges(t *testing.T) {
	cases := [][]string{
		{"/wistia", "resume", "https://t.me/c/4451402994/138", "to", "137"},
		{"/wistia", "resume", "https://t.me/c/4451402994/138", "to", "not-a-number"},
		{"/wistia", "resume", "https://example.com/138", "to", "309"},
		{"/wistia", "resume", "https://t.me/c/4451402994/138"},
	}
	for _, fields := range cases {
		if _, _, _, err := parseWistiaResumeCommand(fields); err == nil {
			t.Errorf("expected error for %#v", fields)
		}
	}
}

func TestFilterWistiaForumRangePreservesTopicTitlesAndOrder(t *testing.T) {
	topics := []forward.ForumTopic{
		{ID: 1, Title: "General", IDs: []int{10, 138, 200}},
		{ID: 2, Title: "05. Backend", IDs: []int{120, 139, 250, 310}},
		{ID: 3, Title: "06. APIs", IDs: []int{138, 180, 309, 400}},
	}
	got := filterWistiaForumRange(topics, 138, 309)
	if len(got) != 3 {
		t.Fatalf("got %d topics, want 3", len(got))
	}
	if got[0].Title != "General" || got[1].Title != "05. Backend" || got[2].Title != "06. APIs" {
		t.Fatalf("topic order/titles changed: %+v", got)
	}
	want := [][]int{{138, 200}, {139, 250}, {138, 180, 309}}
	for i := range want {
		if !reflect.DeepEqual(got[i].IDs, want[i]) {
			t.Errorf("topic %q IDs = %v, want %v", got[i].Title, got[i].IDs, want[i])
		}
	}
}


func TestFilterWistiaForumTopicRangeLocksToLinkedTopic(t *testing.T) {
	topics := []forward.ForumTopic{
		{ID: 4, Title: "03. Frontend", IDs: []int{70, 82, 90, 120, 157, 160}},
		{ID: 7, Title: "05. Backend", IDs: []int{83, 100, 130, 157}},
	}
	got, ok := filterWistiaForumTopicRange(topics, 4, 90, 157)
	if !ok {
		t.Fatal("expected linked Frontend topic")
	}
	if got.ID != 4 || got.Title != "03. Frontend" || !reflect.DeepEqual(got.IDs, []int{90, 120, 157}) {
		t.Fatalf("topic-locked range = %+v", got)
	}
	if _, ok := filterWistiaForumTopicRange(topics, 99, 90, 157); ok {
		t.Fatal("unknown topic should not match")
	}
}

func TestIsWistiaStorageLimitError(t *testing.T) {
	if !isWistiaStorageLimitError(fmt.Errorf("Wistia rejected upload: account has exceeded its storage limit")) {
		t.Fatal("expected storage quota error to be detected")
	}
	if isWistiaStorageLimitError(fmt.Errorf("invalid API token: forbidden")) {
		t.Fatal("ordinary authorization errors must not be treated as quota errors")
	}
}
