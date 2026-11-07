package storage

import ("testing"; "context"; "github.com/gotd/td/tg"
	"github.com/gotd/td/telegram/uploader")

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

func TestUTF16ByteOffsetHandlesAstralCharacters(t *testing.T) {
	value := "A😀B"
	cases := []struct { offset, want int }{{0,0},{1,1},{3,5},{4,6}}
	for _, tc := range cases {
		got, ok := utf16ByteOffset(value, tc.offset)
		if !ok || got != tc.want { t.Errorf("utf16ByteOffset(%q, %d) = (%d,%v), want (%d,true)", value, tc.offset, got, ok, tc.want) }
	}
	if _, ok := utf16ByteOffset(value, 2); ok { t.Fatal("UTF-16 offset inside a surrogate pair must be rejected") }
}

func TestCaptionOptionsKeepsPlainCaptionAndFormatting(t *testing.T) {
	plain := captionOptions("plain caption", nil)
	if len(plain) != 1 || plain[0].Zero() { t.Fatal("expected a plain caption option") }
	formatted := captionOptions("bold text", []tg.MessageEntityClass{&tg.MessageEntityBold{Offset:0,Length:4}})
	if len(formatted) != 1 || formatted[0].Zero() { t.Fatal("expected formatted caption option") }
}

func TestUploadProgressAggregatesConcurrentFileParts(t *testing.T) {
	var current int64
	p:=&uploadProgress{total:100,callback:func(n,total int64){current=n;if total!=100{t.Errorf("total=%d, want 100",total)}}}
	if err:=p.Chunk(context.Background(),uploader.ProgressState{ID:1,Uploaded:30,Total:60});err!=nil{t.Fatal(err)}
	if err:=p.Chunk(context.Background(),uploader.ProgressState{ID:2,Uploaded:20,Total:40});err!=nil{t.Fatal(err)}
	if current!=50{t.Fatalf("aggregate uploaded=%d, want 50",current)}
	if err:=p.Chunk(context.Background(),uploader.ProgressState{ID:1,Uploaded:60,Total:60});err!=nil{t.Fatal(err)}
	if current!=80{t.Fatalf("aggregate uploaded=%d, want 80",current)}
}


func TestChannelPeerUsesAccessHashReturnedByTelegram(t *testing.T) {
	peer, err := channelPeerFromChats([]tg.ChatClass{&tg.Channel{ID:9876543210, AccessHash:123456789}}, 9876543210)
	if err != nil { t.Fatal(err) }
	channel, ok := peer.(*tg.InputPeerChannel)
	if !ok { t.Fatalf("peer type = %T, want *tg.InputPeerChannel", peer) }
	if channel.ChannelID != 9876543210 { t.Fatalf("channel ID = %d, want 9876543210", channel.ChannelID) }
	if channel.AccessHash != 123456789 { t.Fatalf("channel access hash = %d, want Telegram-returned 123456789", channel.AccessHash) }
}

func TestChannelPeerRejectsMissingChannelFromTelegramResponse(t *testing.T) {
	_, err := channelPeerFromChats([]tg.ChatClass{&tg.Channel{ID:1,AccessHash:2}}, 9876543210)
	if err == nil { t.Fatal("expected missing channel error") }
}

func TestResolveTargetBuildsBasicGroupPeer(t *testing.T) {
	peer, err := resolveTarget(context.Background(), nil, "-12345")
	if err != nil { t.Fatal(err) }
	group, ok := peer.(*tg.InputPeerChat)
	if !ok { t.Fatalf("peer type = %T, want *tg.InputPeerChat", peer) }
	if group.ChatID != 12345 { t.Fatalf("chat ID = %d, want 12345", group.ChatID) }
}

func TestResolveTargetRequiresAPIForPrivateChannels(t *testing.T) {
	if _, err := resolveTarget(context.Background(), nil, "-1009876543210"); err == nil {
		t.Fatal("expected channel resolution to require the uploader MTProto API")
	}
}

func TestResolveTargetRejectsInvalidIDs(t *testing.T) {
	for _, target := range []string{"", "12345", "abc", "-1000000000000", "-1009999999999999"} {
		if _, err := resolveTarget(context.Background(), nil, target); err == nil {
			t.Errorf("resolveTarget(%q) expected an error", target)
		}
	}
}

func TestLargeFileUploaderUsesMaximumTelegramPartSize(t *testing.T) {
	if uploadPartSize != 512*1024 {
		t.Fatalf("upload part size = %d, want Telegram maximum 512 KiB", uploadPartSize)
	}
	if uploadThreads != 8 {
		t.Fatalf("upload workers = %d, want configured starting point 8", uploadThreads)
	}
	const largeFileSize int64 = 208 * 1024 * 1024
	parts := (largeFileSize + int64(uploadPartSize) - 1) / int64(uploadPartSize)
	if parts != 416 {
		t.Fatalf("208 MiB file requires %d parts, want 416 at 512 KiB", parts)
	}
}
