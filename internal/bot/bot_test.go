package bot

import ("testing"; "encoding/json"; "strings"; "time"; "github.com/gotd/td/telegram/query/messages"; "github.com/gotd/td/tg")

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

func TestForwardOriginDecodesChannelAndGroup(t *testing.T) {
	for _, raw := range []string{
		`{"type":"channel","chat":{"id":-1001234567890,"title":"Channel"}}`,
		`{"type":"chat","sender_chat":{"id":-1009876543210,"title":"Group"}}`,
	} {
		var origin forwardOrigin
		if err := json.Unmarshal([]byte(raw), &origin); err != nil { t.Fatal(err) }
		chat := origin.sourceChat()
		if chat == nil || chat.ID >= 0 || chat.Title == "" { t.Fatalf("invalid forwarded destination: %+v", chat) }
	}
}

func TestDestinationIsIsolatedPerUser(t *testing.T) {
	b := &Bot{destinations: map[int64]destination{
		11: {ChatID:-100111, Title:"First"},
		22: {ChatID:-100222, Title:"Second"},
	}}
	first, ok := b.getDestination(11)
	if !ok || first.ChatID != -100111 { t.Fatalf("first destination = %+v, %v", first, ok) }
	second, ok := b.getDestination(22)
	if !ok || second.ChatID != -100222 { t.Fatalf("second destination = %+v, %v", second, ok) }
	if _, ok := b.getDestination(33); ok { t.Fatal("unexpected destination for unconfigured user") }
}

func TestAlbumGroupLengthUsesOnlyAdjacentSameGroupMessages(t *testing.T) {
	items := []messages.Elem{
		{Msg:&tg.Message{ID:1,GroupedID:77}},
		{Msg:&tg.Message{ID:2,GroupedID:77}},
		{Msg:&tg.Message{ID:3,GroupedID:77}},
		{Msg:&tg.Message{ID:4,GroupedID:88}},
	}
	if got := albumGroupLength(items,0); got != 3 { t.Fatalf("group length = %d, want 3",got) }
	if got := albumGroupLength(items,2); got != 1 { t.Fatalf("group length at next album = %d, want 1",got) }
	if got := albumGroupLength(items,3); got != 1 { t.Fatalf("group length for singleton = %d, want 1",got) }
}

func TestFormatProgressShowsCountsStageAndETA(t *testing.T) {
	got := formatProgress("Batch","Uploading",3,4,1,1,0,10*time.Second)
	for _, want := range []string{"Batch · 50%","Stage: Uploading","Item: 3/4","Uploaded: 1 · Failed: 1 · Skipped: 0","Elapsed: 10s","ETA: 10s"} {
		if !strings.Contains(got,want) { t.Errorf("progress text %q does not contain %q",got,want) }
	}
}

func TestFormatProgressCompletesAtHundredPercent(t *testing.T) {
	got := formatProgress("Copy","Finished",4,4,3,1,0,12*time.Second)
	if !strings.Contains(got,"Copy · 100%") || !strings.Contains(got,"ETA: 0s") { t.Fatalf("unexpected completed progress: %s",got) }
}
