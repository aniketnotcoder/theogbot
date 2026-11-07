package storage

import ("testing"; "context")

func TestNewAllowsPerUserDestinationConfiguration(t *testing.T) {
	client, err := New(12345, "api-hash", "bot-token", "")
	if err != nil { t.Fatalf("New should allow an empty shared target when users configure destinations: %v", err) }
	if client.Target() != "" { t.Fatalf("unexpected shared target %q", client.Target()) }
}

func TestNewStillRequiresMTProtoAndBotCredentials(t *testing.T) {
	if _, err := New(0, "api-hash", "bot-token", ""); err == nil { t.Fatal("expected invalid API ID to fail") }
	if _, err := New(12345, "", "bot-token", ""); err == nil { t.Fatal("expected missing API hash to fail") }
	if _, err := New(12345, "api-hash", "", ""); err == nil { t.Fatal("expected missing bot token to fail") }
}

func TestUploadAlbumRejectsInvalidItemCountsBeforeConnecting(t *testing.T) {
	client, err := New(12345,"api-hash","bot-token","")
	if err != nil { t.Fatal(err) }
	if _, err := client.UploadAlbumTo(context.Background(), nil, nil); err == nil { t.Fatal("expected empty album to be rejected") }
	items := make([]AlbumItem, 11)
	if _, err := client.UploadAlbumTo(context.Background(), nil, items); err == nil { t.Fatal("expected album larger than ten to be rejected") }
}
