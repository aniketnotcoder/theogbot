package links

import (
	"errors"
	"testing"
)

func TestParseMessageLinks(t *testing.T) {
	tests := []struct {
		name string
		input string
		want MessageLink
	}{
		{"public", "https://t.me/example_channel/42", MessageLink{Username: "example_channel", MessageID: 42}},
		{"preview", "https://t.me/s/example_channel/42", MessageLink{Username: "example_channel", MessageID: 42}},
		{"telegram domain", "https://telegram.me/example_channel/42?single", MessageLink{Username: "example_channel", MessageID: 42}},
		{"private", "https://t.me/c/123456789/42", MessageLink{ChannelID: 123456789, MessageID: 42}},
		{"tg private post", "tg://privatepost?channel=123456789&post=42", MessageLink{ChannelID: 123456789, MessageID: 42}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if err != nil { t.Fatalf("Parse() error = %v", err) }
			if got != tt.want { t.Fatalf("Parse() = %#v, want %#v", got, tt.want) }
		})
	}
}

func TestParseRejectsNonMessageLinks(t *testing.T) {
	for _, input := range []string{
		"https://t.me/+invitehash",
		"https://t.me/example_channel",
		"https://evil.example/example_channel/42",
		"https://t.me/c/not-a-number/42",
		"https://t.me/example_channel/0",
		"not a URL",
	} {
		if _, err := Parse(input); !errors.Is(err, ErrUnsupportedLink) {
			t.Errorf("Parse(%q) error = %v, want ErrUnsupportedLink", input, err)
		}
	}
}

func TestParseForumMessagePermalinks(t *testing.T) {
	tests := []struct {
		name string
		input string
		want MessageLink
	}{
		{"public forum message", "https://t.me/example_channel/55/138", MessageLink{Username: "example_channel", MessageID: 138}},
		{"private forum message", "https://t.me/c/123456789/55/138", MessageLink{ChannelID: 123456789, MessageID: 138}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.input)
			if err != nil { t.Fatalf("Parse() error = %v", err) }
			if got != tt.want { t.Fatalf("Parse() = %#v, want %#v", got, tt.want) }
		})
	}
}
