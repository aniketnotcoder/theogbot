package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aniketnotcoder/theogbot/internal/forward"
	"github.com/aniketnotcoder/theogbot/internal/links"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

func (b *Bot) runWistia(parent context.Context, userID, chatID int64, rawLink, mode string) {
	ctx := parent
	if b.wistia == nil {
		_ = b.sendMessage(ctx, chatID, "Wistia upload is not configured on this deployment.")
		return
	}
	if b.accounts == nil {
		_ = b.sendMessage(ctx, chatID, "Telegram MTProto is unavailable. Connect your Telegram account first.")
		return
	}
	api, ok := b.accounts.API(userID)
	if !ok {
		_ = b.sendMessage(ctx, chatID, "Connect your Telegram account first using /login.")
		return
	}
	parsed, err := links.Parse(rawLink)
	if err != nil {
		_ = b.sendMessage(ctx, chatID, "Invalid Telegram post link. Use a public or accessible private post link, for example https://t.me/channel/123.")
		return
	}
	var source tg.InputPeerClass
	if parsed.Username != "" {
		source, err = forward.ResolveUsername(ctx, api, parsed.Username)
	} else {
		source, err = forward.ResolvePrivateChannelID(ctx, api, parsed.ChannelID)
	}
	if err != nil {
		_ = b.sendMessage(ctx, chatID, "Cannot access the Telegram source: "+err.Error())
		return
	}
	elements, err := forward.FetchMessagesByID(ctx, api, source, []int{parsed.MessageID})
	if err != nil {
		_ = b.sendMessage(ctx, chatID, "Could not retrieve that Telegram post: "+err.Error())
		return
	}
	if len(elements) != 1 {
		_ = b.sendMessage(ctx, chatID, "The requested Telegram post was not found.")
		return
	}
	elem := elements[0]
	if msg, ok := elem.Msg.(*tg.Message); !ok || msg.Noforwards {
		_ = b.sendMessage(ctx, chatID, "This post is not accessible for upload (it may be protected).")
		return
	}
	progress, progressErr := b.newProgress(ctx, chatID, "Wistia upload", 1)
	if progressErr != nil {
		b.log.Warn("Wistia progress message could not be created", "user_id", userID, "error", progressErr)
		_ = b.sendMessage(ctx, chatID, "Starting Wistia upload. Progress reporting is unavailable for this job.")
	} else {
		progress.update(ctx, "Preparing Telegram source", 1, 0, 0, 0, true)
	}

	var resultURL, hashedID, status string
	switch mode {
	case "parallel":
		if progress != nil {
			progress.update(ctx, "Downloading with parallel workers", 1, 0, 0, 0, true)
		}
		result, tempPath, uploadErr := b.wistia.UploadTelegramMessageFromFile(
			ctx, api, elem, b.tempDir,
			func(stage string, current, total int64) {
				if progress != nil {
					progress.updateBytes(ctx, stage, current, total, 1, 0, 0, 0)
				}
			},
		)
		if uploadErr != nil {
			if tempPath != "" {
				b.log.Error("Wistia parallel upload failed; source file retained for reconciliation", "user_id", userID, "path", tempPath, "error", uploadErr)
			}
			b.finishWistiaProgress(ctx, progress, false)
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Wistia parallel upload failed: %v", uploadErr))
			return
		}
		resultURL, hashedID, status = result.PlayerURL, result.HashedID, result.Status
	case "stream":
		if progress != nil {
			progress.update(ctx, "Streaming to Wistia (zero disk)", 1, 0, 0, 0, true)
		}
		result, uploadErr := b.wistia.UploadTelegramMessage(ctx, api, elem, func(current, total int64) {
			if progress != nil {
				progress.updateBytes(ctx, "Streaming to Wistia", current, total, 1, 0, 0, 0)
			}
		})
		if uploadErr != nil {
			b.finishWistiaProgress(ctx, progress, false)
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Wistia streaming upload failed: %v", uploadErr))
			return
		}
		resultURL, hashedID, status = result.PlayerURL, result.HashedID, result.Status
	default:
		b.finishWistiaProgress(ctx, progress, false)
		_ = b.sendMessage(ctx, chatID, "Unknown Wistia transfer mode.")
		return
	}

	if b.wistia.HasDataAPI() {
		if progress != nil {
			progress.update(ctx, "Wistia processing", 1, 0, 0, 0, true)
		}
		waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		info, waitErr := b.wistia.WaitUntilReady(waitCtx, hashedID)
		cancel()
		if waitErr != nil {
			b.finishWistiaProgress(ctx, progress, false)
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Wistia accepted the upload, but processing status could not be confirmed: %v\nMedia ID: %s\nPlayer URL: %s", waitErr, hashedID, resultURL))
			return
		}
		status = info.Status
	} else if strings.TrimSpace(status) == "" {
		status = "uploaded; processing status not checked (Data API read token not configured)"
	}
	b.finishWistiaProgress(ctx, progress, true)
	_ = b.sendMessage(ctx, chatID, fmt.Sprintf("Wistia upload successful!\n\nStatus: %s\nMedia ID: %s\nPlayer URL: %s\nEmbed URL: %s", status, hashedID, resultURL, resultURL))
}

func (b *Bot) finishWistiaProgress(ctx context.Context, progress *progressReporter, success bool) {
	if progress == nil {
		return
	}
	stage := "Failed"
	done, failed := 0, 1
	if success {
		stage, done, failed = "Finished", 1, 0
	}
	progress.update(ctx, stage, 1, done, failed, 0, true)
}

// Keep the messages.Elem type referenced here so command tests can build
// fixtures without importing gotd's internal query conversion helpers.
var _ messages.Elem
