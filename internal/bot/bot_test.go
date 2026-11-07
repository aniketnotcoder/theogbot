package bot

import ("context"; "testing"; "encoding/json"; "strings"; "time"; "github.com/gotd/td/telegram/query/messages"; "github.com/gotd/td/tg")

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

func TestDestinationMessageRefsForPublicChannel(t *testing.T) {
	updates := &tg.Updates{Updates: []tg.UpdateClass{
		&tg.UpdateNewChannelMessage{Message:&tg.Message{ID:41}},
		&tg.UpdateNewChannelMessage{Message:&tg.Message{ID:42}},
	}}
	got := destinationMessageRefs(destination{ChatID:-1001234567890,Username:"sample_channel"},updates)
	want := []string{"https://t.me/sample_channel/41","https://t.me/sample_channel/42"}
	if strings.Join(got,"|") != strings.Join(want,"|") { t.Fatalf("refs = %v, want %v",got,want) }
}
func TestDestinationMessageRefsForPrivateChannel(t *testing.T) {
	updates := &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewChannelMessage{Message:&tg.Message{ID:88}}}}
	got := destinationMessageRefs(destination{ChatID:-1009876543210},updates)
	if len(got)!=1 || got[0]!="https://t.me/c/9876543210/88" { t.Fatalf("refs = %v",got) }
}
func TestDestinationMessageRefsForBasicGroupUsesIdentifier(t *testing.T) {
	updates := &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewMessage{Message:&tg.Message{ID:17}}}}
	got := destinationMessageRefs(destination{ChatID:-12345},updates)
	if len(got)!=1 || got[0]!="chat -12345 · message 17" { t.Fatalf("refs = %v",got) }
}
func TestDestinationMessageRefsSupportsShortSentMessage(t *testing.T) {
	got := destinationMessageRefs(destination{ChatID:-1001234567890},&tg.UpdateShortSentMessage{ID:19})
	if len(got)!=1 || got[0]!="https://t.me/c/1234567890/19" { t.Fatalf("refs = %v",got) }
}

func TestHumanBytesAndSpeed(t *testing.T) {
	if got:=humanBytes(1024);got!="1.0 KiB"{t.Fatalf("humanBytes(1024)=%q",got)}
	if got:=humanSpeed(0);got!="calculating…"{t.Fatalf("humanSpeed(0)=%q",got)}
	if got:=humanSpeed(2048);got!="2.0 KiB/s"{t.Fatalf("humanSpeed(2048)=%q",got)}
}

func TestProgressSpeedUsesOneSecondWindowAndResetsBetweenStages(t *testing.T) {
	p := &progressReporter{}
	start := time.Unix(100, 0)
	p.recordByteSample("Downloading", 0, 20<<20, start)
	p.recordByteSample("Downloading", 1<<20, 20<<20, start.Add(100*time.Millisecond))
	if p.speed != 0 { t.Fatalf("speed after a 100ms burst = %f, want no unstable sample", p.speed) }
	p.recordByteSample("Downloading", 10<<20, 20<<20, start.Add(time.Second))
	if p.speed < 9<<20 || p.speed > 11<<20 { t.Fatalf("one-second throughput = %f, want about 10 MiB/s", p.speed) }
	p.recordByteSample("Uploading", 0, 20<<20, start.Add(2*time.Second))
	if p.speed != 0 { t.Fatalf("speed after stage reset = %f, want 0", p.speed) }
}

func TestProgressBarIsClampedAndConsistent(t *testing.T) {
	if got:=progressBar(0);got!="················"{t.Fatalf("zero bar = %q",got)}
	if got:=progressBar(100);got!="━━━━━━━━━━━━━━━━"{t.Fatalf("full bar = %q",got)}
	if got:=progressBar(200);got!=progressBar(100){t.Fatalf("bar should clamp above 100: %q",got)}
	if got:=progressBar(-1);got!=progressBar(0){t.Fatalf("bar should clamp below 0: %q",got)}
}

func TestProgressMessageIsCleanAndUsesPhaseBytes(t *testing.T) {
	got:=formatProgress("Batch","Uploading",4,10,3,0,0,4,10,2*1024*1024,2*time.Minute)
	for _,want:=range []string{"BATCH","Uploading  ·  40%","4 of 10 items","4.0 KiB of 10.0 KiB","2.0 MiB/s","left"} {
		if !strings.Contains(got,want){t.Fatalf("progress missing %q: %s",want,got)}
	}
	for _,unwanted:=range []string{"Stage:","Bytes:","Speed:","Elapsed:"} {
		if strings.Contains(got,unwanted){t.Fatalf("debug label %q leaked into progress: %s",unwanted,got)}
	}
}

func TestProgressCompletionHasNoDebugFields(t *testing.T) {
	got:=formatProgress("Copy","Finished",5,5,5,0,0,0,0,0,75*time.Second)
	for _,want:=range []string{"TRANSFER COMPLETE","100%","5 of 5 items","Completed in 1m15s"} {
		if !strings.Contains(got,want){t.Fatalf("completion missing %q: %s",want,got)}
	}
}

func TestProgressResetsPerFilePhase(t *testing.T) {
	p:=&progressReporter{total:2,displayStage:"Uploading",bytesCurrent:900,bytesTotal:900,speed:100}
	p.update(context.Background(),"Downloading",1,0,0,0,true)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bytesCurrent!=0||p.bytesTotal!=0||p.speed!=0 {t.Fatalf("stale upload progress carried into download: current=%d total=%d speed=%f",p.bytesCurrent,p.bytesTotal,p.speed)}
}
