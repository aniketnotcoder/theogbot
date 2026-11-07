package bot

import "testing"

func TestParseChatLink(t *testing.T) {
	tests := []struct {
		name string
		input string
		wantUsername string
		wantChannelID int64
		wantErr bool
	}{
		{name: "public chat", input: "https://t.me/example_channel", wantUsername: "example_channel"},
		{name: "public post permalink identifies chat", input: "https://t.me/example_channel/42", wantUsername: "example_channel"},
		{name: "private channel", input: "https://t.me/c/123456789", wantChannelID: 123456789},
		{name: "private post permalink identifies chat", input: "https://t.me/c/123456789/42", wantChannelID: 123456789},
		{name: "telegram.me domain", input: "https://telegram.me/example_channel/42", wantUsername: "example_channel"},
		{name: "unsupported host", input: "https://example.com/example_channel", wantErr: true},
		{name: "invalid private id", input: "https://t.me/c/not-a-number", wantErr: true},
		{name: "missing chat", input: "https://t.me/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUsername, gotChannelID, err := parseChatLink(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseChatLink() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil { return }
			if gotUsername != tt.wantUsername || gotChannelID != tt.wantChannelID {
				t.Fatalf("parseChatLink() = (%q, %d), want (%q, %d)", gotUsername, gotChannelID, tt.wantUsername, tt.wantChannelID)
			}
		})
	}
}
