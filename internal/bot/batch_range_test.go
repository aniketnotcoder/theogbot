package bot

import (
	"reflect"
	"testing"
)

func TestParseBatchCommandExpandsInclusiveRanges(t *testing.T) {
	got, err := parseBatchCommand([]string{"/batch", "https://t.me/c/4332971248/", "61", "to", "63"})
	if err != nil { t.Fatal(err) }
	want := []string{"https://t.me/c/4332971248/61", "https://t.me/c/4332971248/62", "https://t.me/c/4332971248/63"}
	if !reflect.DeepEqual(got, want) { t.Fatalf("range = %#v, want %#v", got, want) }
}

func TestParseBatchCommandSupportsHyphenRangeAndPublicChannel(t *testing.T) {
	got, err := parseBatchCommand([]string{"/batch", "https://t.me/sample_channel", "7-9"})
	if err != nil { t.Fatal(err) }
	want := []string{"https://t.me/sample_channel/7", "https://t.me/sample_channel/8", "https://t.me/sample_channel/9"}
	if !reflect.DeepEqual(got, want) { t.Fatalf("range = %#v, want %#v", got, want) }
}

func TestParseBatchCommandKeepsLegacyPostLinks(t *testing.T) {
	input := []string{"/batch", "https://t.me/sample_channel/7", "https://t.me/sample_channel/9"}
	got, err := parseBatchCommand(input)
	if err != nil { t.Fatal(err) }
	if !reflect.DeepEqual(got, input[1:]) { t.Fatalf("legacy links = %#v", got) }
}

func TestParseBatchCommandRejectsInvalidRanges(t *testing.T) {
	cases := [][]string{
		{"/batch", "https://t.me/sample_channel", "0", "to", "9"},
		{"/batch", "https://t.me/sample_channel", "9", "to", "2"},
		{"/batch", "https://t.me/sample_channel", "1-50001"},
		{"/batch", "https://example.com/channel", "1", "to", "3"},
	}
	for _, fields := range cases {
		if _, err := parseBatchCommand(fields); err == nil { t.Errorf("expected invalid range error for %#v", fields) }
	}
}

func TestParseBatchCommandRequiresArguments(t *testing.T) {
	if _, err := parseBatchCommand([]string{"/batch"}); err == nil { t.Fatal("expected usage error") }
}
