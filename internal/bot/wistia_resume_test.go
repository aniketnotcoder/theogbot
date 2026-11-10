package bot

import (
	"reflect"
	"testing"

	"github.com/aniketnotcoder/theogbot/internal/forward"
)

func TestParseWistiaResumeCommandUsesFailedPostIDAsStart(t *testing.T) {
	link, start, end, err := parseWistiaResumeCommand([]string{
		"/wistia", "resume", "https://t.me/c/4451402994/138", "to", "309",
	})
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://t.me/c/4451402994/138" || start != 138 || end != 309 {
		t.Fatalf("got (%q, %d, %d), want failed post 138 through 309", link, start, end)
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
