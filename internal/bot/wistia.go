package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aniketnotcoder/theogbot/internal/forward"
	"github.com/aniketnotcoder/theogbot/internal/links"
	"github.com/aniketnotcoder/theogbot/internal/wistia"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

type wistiaBatchResult struct {
	SourceID int
	Name string
	HashedID string
	Status string
	DeliveryURL string
	MP4URL string
}

func (b *Bot) requireWistia(ctx context.Context, chatID int64) bool {
	if b.wistia == nil {
		_ = b.sendMessage(ctx, chatID, "Wistia upload is not configured. Set WISTIA_API_TOKEN and WISTIA_PROJECT_ID.")
		return false
	}
	if !b.wistia.HasDataAPI() {
		_ = b.sendMessage(ctx, chatID, "Direct .bin/.mp4 delivery links require WISTIA_DATA_API_TOKEN with Read all folder and media data permission. Set it in Railway and redeploy.")
		return false
	}
	if b.accounts == nil {
		_ = b.sendMessage(ctx, chatID, "Telegram MTProto is unavailable. Connect your Telegram account first.")
		return false
	}
	return true
}

func (b *Bot) resolveWistiaSource(ctx context.Context, api *tg.Client, rawLink string) (tg.InputPeerClass, int, error) {
	parsed, err := links.Parse(rawLink)
	if err != nil { return nil, 0, fmt.Errorf("invalid Telegram post link %q", rawLink) }
	var source tg.InputPeerClass
	if parsed.Username != "" {
		source, err = forward.ResolveUsername(ctx, api, parsed.Username)
	} else {
		source, err = forward.ResolvePrivateChannelID(ctx, api, parsed.ChannelID)
	}
	if err != nil { return nil, 0, fmt.Errorf("cannot access source chat: %w", err) }
	return source, parsed.MessageID, nil
}

func (b *Bot) runWistia(parent context.Context, userID, chatID int64, rawLink, mode string) {
	ctx := parent
	if !b.requireWistia(ctx, chatID) { return }
	api, ok := b.accounts.API(userID)
	if !ok { _ = b.sendMessage(ctx, chatID, "Connect your Telegram account first using /login."); return }
	source, messageID, err := b.resolveWistiaSource(ctx, api, rawLink)
	if err != nil { _ = b.sendMessage(ctx, chatID, err.Error()); return }
	elements, err := forward.FetchMessagesByID(ctx, api, source, []int{messageID})
	if err != nil { _ = b.sendMessage(ctx, chatID, "Could not retrieve that Telegram post: "+err.Error()); return }
	if len(elements) != 1 { _ = b.sendMessage(ctx, chatID, "The requested Telegram post was not found."); return }
	elem := elements[0]
	if !isWistiaVideo(elem) {
		_ = b.sendMessage(ctx, chatID, "That Telegram post does not contain an accessible video document.")
		return
	}
	progress, progressErr := b.newProgress(ctx, chatID, "Wistia upload", 1)
	if progressErr != nil {
		b.log.Warn("Wistia progress message could not be created", "user_id", userID, "error", progressErr)
		_ = b.sendMessage(ctx, chatID, "Starting Wistia upload. Progress reporting is unavailable for this job.")
	} else { progress.update(ctx, "Preparing Telegram source", 1, 0, 0, 0, true) }

	var result wistia.UploadResult
	switch mode {
	case "parallel", "":
		if progress != nil { progress.update(ctx, "Downloading with parallel workers", 1, 0, 0, 0, true) }
		result, _, err = b.wistia.UploadTelegramMessageFromFile(ctx, api, elem, b.tempDir, func(stage string, current, total int64) {
			if progress != nil { progress.updateBytes(ctx, stage, current, total, 1, 0, 0, 0) }
		})
	case "stream":
		if progress != nil { progress.update(ctx, "Streaming to Wistia", 1, 0, 0, 0, true) }
		result, err = b.wistia.UploadTelegramMessage(ctx, api, elem, func(current, total int64) {
			if progress != nil { progress.updateBytes(ctx, "Streaming to Wistia", current, total, 1, 0, 0, 0) }
		})
	default:
		err = fmt.Errorf("unknown transfer mode %q; use parallel or stream", mode)
	}
	if err != nil {
		b.finishWistiaProgress(ctx, progress, false)
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Wistia upload failed: %v", err))
		return
	}
	if progress != nil { progress.update(ctx, "Wistia processing", 1, 0, 0, 0, true) }
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	info, waitErr := b.wistia.WaitUntilReady(waitCtx, result.HashedID)
	cancel()
	if waitErr != nil {
		b.finishWistiaProgress(ctx, progress, false)
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Wistia accepted the upload, but its direct delivery asset is not ready: %v\nMedia ID: %s", waitErr, result.HashedID))
		return
	}
	b.finishWistiaProgress(ctx, progress, true)
	_ = b.sendMessage(ctx, chatID, formatWistiaResult(wistiaBatchResult{SourceID: messageID, Name: info.Name, HashedID: info.HashedID, Status: info.Status, DeliveryURL: info.DeliveryURL, MP4URL: info.MP4URL}))
}

// runWistiaLinks uploads an explicit list/range of source posts. All links must
// resolve to one Telegram chat, matching /batch's safety and ordering rules.
func (b *Bot) runWistiaLinks(parent context.Context, userID, chatID int64, rawLinks []string) {
	ctx := parent
	if !b.requireWistia(ctx, chatID) { return }
	api, ok := b.accounts.API(userID)
	if !ok { _ = b.sendMessage(ctx, chatID, "Connect your Telegram account first using /login."); return }
	var source tg.InputPeerClass
	ids := make([]int, 0, len(rawLinks))
	resolved := make(map[string]tg.InputPeerClass)
	for _, raw := range rawLinks {
		parsed, err := links.Parse(raw)
		if err != nil { _ = b.sendMessage(ctx, chatID, "Invalid Telegram post link: "+raw); return }
		key := fmt.Sprintf("channel:%d", parsed.ChannelID)
		if parsed.Username != "" { key = "username:"+strings.ToLower(parsed.Username) }
		peer := resolved[key]
		if peer == nil {
			if parsed.Username != "" { peer, err = forward.ResolveUsername(ctx, api, parsed.Username) } else { peer, err = forward.ResolvePrivateChannelID(ctx, api, parsed.ChannelID) }
			if err != nil { _ = b.sendMessage(ctx, chatID, "Cannot access source chat: "+err.Error()); return }
			resolved[key] = peer
		}
		if source != nil && peerKey(source) != peerKey(peer) { _ = b.sendMessage(ctx, chatID, "All /wistia batch links must belong to the same source chat."); return }
		source = peer
		ids = append(ids, parsed.MessageID)
	}
	b.uploadWistiaIDs(ctx, userID, api, source, chatID, ids, "Wistia batch", "")
}

// runWistiaCopy mirrors /copy: it reads accessible history, detects Telegram
// forums, and keeps each topic's title in the uploaded Wistia media name.
func (b *Bot) runWistiaCopy(parent context.Context, userID, chatID int64, rawChat string, limit int) {
	ctx := parent
	if !b.requireWistia(ctx, chatID) { return }
	api, ok := b.accounts.API(userID)
	if !ok { _ = b.sendMessage(ctx, chatID, "Connect your Telegram account first using /login."); return }
	username, channelID, err := parseChatLink(rawChat)
	if err != nil { _ = b.sendMessage(ctx, chatID, err.Error()); return }
	var source tg.InputPeerClass
	if username != "" { source, err = forward.ResolveUsername(ctx, api, username) } else { source, err = forward.ResolvePrivateChannelID(ctx, api, channelID) }
	if err != nil { _ = b.sendMessage(ctx, chatID, "Cannot access source chat: "+err.Error()); return }
	_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Scanning accessible history and forum topics for videos (limit %d)…", limit))
	topics, forumErr := forward.FetchForumTopics(ctx, api, source, limit)
	if forumErr == nil {
		total := 0
		for _, topic := range topics { total += len(topic.IDs) }
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Forum detected: %d topics, %d source messages. Video uploads will retain each topic title in the Wistia media name.", len(topics), total))
		for _, topic := range topics {
			if ctx.Err() != nil { break }
			title := strings.TrimSpace(topic.Title)
			if title == "" { title = fmt.Sprintf("Topic %d", topic.ID) }
			if len(topic.IDs) > 0 { b.uploadWistiaIDs(ctx, userID, api, source, chatID, topic.IDs, "Wistia · "+title, title) }
		}
		_ = b.sendMessage(ctx, chatID, "Wistia forum copy finished. Topic labels were retained in media names.")
		return
	}
	if !errors.Is(forumErr, forward.ErrNotForum) {
		_ = b.sendMessage(ctx, chatID, "Forum inspection failed: "+forumErr.Error())
		return
	}
	ids, err := forward.FetchHistoryIDs(ctx, api, source, limit)
	if err != nil { _ = b.sendMessage(ctx, chatID, "History retrieval failed: "+err.Error()); return }
	if len(ids) == 0 { _ = b.sendMessage(ctx, chatID, "No accessible messages found in source history."); return }
	b.uploadWistiaIDs(ctx, userID, api, source, chatID, ids, "Wistia copy", "")
}

// runWistiaResume continues a partially completed Wistia migration from an
// inclusive Telegram message-ID range. For forums, it resolves every message
// back to its topic and keeps topic labels in Wistia media names.
func (b *Bot) runWistiaResume(parent context.Context, userID, chatID int64, rawLink string, start, end int) {
	ctx := parent
	if !b.requireWistia(ctx, chatID) { return }
	api, ok := b.accounts.API(userID)
	if !ok { _ = b.sendMessage(ctx, chatID, "Connect your Telegram account first using /login."); return }
	source, linkedID, err := b.resolveWistiaSource(ctx, api, rawLink)
	if err != nil { _ = b.sendMessage(ctx, chatID, err.Error()); return }
	if linkedID != start || start <= 0 || end < start {
		_ = b.sendMessage(ctx, chatID, "Resume range does not match the linked Telegram post.")
		return
	}
	_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Resuming Wistia migration from Telegram message %d through %d. Previously completed messages before %d will not be uploaded again.", start, end, start))

	topics, forumErr := forward.FetchForumTopics(ctx, api, source, 50000)
	if forumErr == nil {
		selected := filterWistiaForumRange(topics, start, end)
		if len(selected) == 0 {
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("No accessible forum messages found in range %d-%d.", start, end))
			return
		}
		total := 0
		for _, topic := range selected { total += len(topic.IDs) }
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Forum resume: %d topics and %d source messages in range. Topic labels will be retained.", len(selected), total))
		for _, topic := range selected {
			if ctx.Err() != nil { break }
			title := strings.TrimSpace(topic.Title)
			if title == "" { title = fmt.Sprintf("Topic %d", topic.ID) }
			b.uploadWistiaIDs(ctx, userID, api, source, chatID, topic.IDs, "Wistia resume · "+title, title)
		}
		if ctx.Err() == nil { _ = b.sendMessage(ctx, chatID, "Wistia forum resume finished. Topic labels were retained in media names.") }
		return
	}
	if !errors.Is(forumErr, forward.ErrNotForum) {
		_ = b.sendMessage(ctx, chatID, "Forum inspection failed; resume stopped without uploading: "+forumErr.Error())
		return
	}

	ids, err := forward.FetchHistoryIDs(ctx, api, source, 50000)
	if err != nil { _ = b.sendMessage(ctx, chatID, "History retrieval failed: "+err.Error()); return }
	selected := make([]int, 0)
	for _, id := range ids { if id >= start && id <= end { selected = append(selected, id) } }
	if len(selected) == 0 {
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("No accessible messages found in range %d-%d.", start, end))
		return
	}
	b.uploadWistiaIDs(ctx, userID, api, source, chatID, selected, "Wistia resume", "")
}

func isWistiaVideo(elem messages.Elem) bool {
	if _, ok := elem.Msg.(*tg.Message); !ok { return false }
	doc, ok := elem.Document()
	if !ok || doc == nil { return false }
	if strings.HasPrefix(strings.ToLower(doc.MimeType), "video/") { return true }
	for _, attr := range doc.Attributes { if _, ok := attr.(*tg.DocumentAttributeVideo); ok { return true } }
	return false
}

func (b *Bot) uploadWistiaIDs(ctx context.Context, userID int64, api *tg.Client, source tg.InputPeerClass, chatID int64, ids []int, label, topicTitle string) {
	if len(ids) == 0 { _ = b.sendMessage(ctx, chatID, label+": no source messages to process."); return }
	elements, err := forward.FetchMessagesByID(ctx, api, source, ids)
	if err != nil { _ = b.sendMessage(ctx, chatID, label+" could not retrieve the requested source messages: "+err.Error()); return }
	progress, progressErr := b.newProgress(ctx, chatID, label, len(elements))
	if progressErr != nil { b.log.Warn("Wistia batch progress unavailable", "user_id", userID, "error", progressErr) }
	done, failed, skipped := 0, 0, 0
	results := make([]wistiaBatchResult, 0)
	for i, elem := range elements {
		if err := ctx.Err(); err != nil { break }
		msg, ok := elem.Msg.(*tg.Message)
		if !ok || msg.Noforwards || !isWistiaVideo(elem) {
			skipped++
			if progress != nil { progress.update(ctx, "Skipping non-video or protected post", i+1, done, failed, skipped, false) }
			continue
		}
		filename := "Telegram video"
		if file, ok := elem.File(); ok && file.Name != "" { filename = file.Name }
		name := strings.TrimSpace(msg.Message)
		if name == "" { name = filename }
		if topicTitle != "" { name = topicTitle+" · "+name }
		if len(name) > 250 { name = name[:250] }
		if progress != nil { progress.update(ctx, "Downloading with parallel workers", i+1, done, failed, skipped, true) }
		uploaded, tempPath, uploadErr := b.wistia.UploadTelegramMessageFromFileNamed(ctx, api, elem, b.tempDir, name, func(stage string, current, total int64) {
			if progress != nil { progress.updateBytes(ctx, stage, current, total, i+1, done, failed, skipped) }
		})
		if uploadErr != nil {
			failed++
			if tempPath != "" { b.log.Error("Wistia batch failed; retained temporary source for reconciliation", "user_id", userID, "message_id", msg.ID, "path", tempPath, "error", uploadErr) }
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s [%d/%d] failed for Telegram message %d: %v", label, i+1, len(elements), msg.ID, uploadErr))
			continue
		}
		done++
		results = append(results, wistiaBatchResult{SourceID: msg.ID, Name: name, HashedID: uploaded.HashedID, Status: uploaded.Status})
		if progress != nil { progress.update(ctx, "Uploaded; waiting for Wistia processing", i+1, done, failed, skipped, true) }
	}
	if ctx.Err() != nil {
		if progress != nil { progress.update(context.Background(), "Cancelled", len(elements), done, failed, skipped, true) }
		_ = b.sendMessage(context.Background(), chatID, fmt.Sprintf("%s cancelled. Uploaded: %d, failed: %d, skipped: %d, requested: %d.", label, done, failed, skipped, len(elements)))
	} else {
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s upload pass complete. Uploaded: %d, failed: %d, skipped: %d, requested: %d. Waiting for Wistia assets…", label, done, failed, skipped, len(elements)))
	}

	// Poll Wistia processing with a small bounded pool after all file uploads
	// have completed. This keeps disk usage bounded while avoiding one full
	// processing wait per video in a large batch.
	var waitGroup sync.WaitGroup
	waitSlots := make(chan struct{}, 4)
	for i := range results {
		if ctx.Err() != nil { break }
		waitSlots <- struct{}{}
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			defer func() { <-waitSlots }()
			waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
			defer cancel()
			info, waitErr := b.wistia.WaitUntilReady(waitCtx, results[index].HashedID)
			if waitErr != nil {
				results[index].Status = "uploaded; asset not ready: "+waitErr.Error()
				return
			}
			results[index].Name, results[index].Status = info.Name, info.Status
			results[index].DeliveryURL, results[index].MP4URL = info.DeliveryURL, info.MP4URL
		}(i)
	}
	waitGroup.Wait()
	ready := 0
	for _, item := range results { if item.DeliveryURL != "" && item.MP4URL != "" { ready++ } }
	finishCtx := ctx
	if ctx.Err() != nil { finishCtx = context.Background() }
	if progress != nil { progress.update(finishCtx, "Finished", len(elements), done, failed, skipped, true) }
	_ = b.sendMessage(finishCtx, chatID, fmt.Sprintf("%s finished. Uploaded: %d, assets ready: %d, failed: %d, skipped: %d, requested: %d.", label, done, ready, failed, skipped, len(elements)))
	sendWistiaBatchResults(finishCtx, b, chatID, label, results)
}

func sendWistiaBatchResults(ctx context.Context, b *Bot, chatID int64, label string, results []wistiaBatchResult) {
	if len(results) == 0 { return }
	const perMessage = 5
	for start := 0; start < len(results); start += perMessage {
		end := start + perMessage
		if end > len(results) { end = len(results) }
		var out strings.Builder
		fmt.Fprintf(&out, "%s direct delivery links (%d-%d of %d):\n\n", label, start+1, end, len(results))
		for _, item := range results[start:end] {
			fmt.Fprintf(&out, "Telegram message: %d\nName: %s\nStatus: %s\nMedia ID: %s\n", item.SourceID, item.Name, item.Status, item.HashedID)
			if item.DeliveryURL != "" { fmt.Fprintf(&out, ".bin: %s\n", item.DeliveryURL) }
			if item.MP4URL != "" { fmt.Fprintf(&out, ".mp4: %s\n", item.MP4URL) }
			if item.DeliveryURL == "" { out.WriteString("Direct asset URL is not available yet.\n") }
			out.WriteString("\n")
		}
		_ = b.sendMessage(ctx, chatID, strings.TrimSpace(out.String()))
	}
}

func formatWistiaResult(item wistiaBatchResult) string {
	return fmt.Sprintf("Wistia upload successful!\n\nStatus: %s\nMedia ID: %s\n.bin delivery URL: %s\n.mp4 delivery URL: %s", item.Status, item.HashedID, item.DeliveryURL, item.MP4URL)
}

func (b *Bot) finishWistiaProgress(ctx context.Context, progress *progressReporter, success bool) {
	if progress == nil { return }
	stage, done, failed := "Failed", 0, 1
	if success { stage, done, failed = "Finished", 1, 0 }
	progress.update(ctx, stage, 1, done, failed, 0, true)
}
