package bot

import (
	"context"
	"fmt"

	"github.com/aniketnotcoder/theogbot/internal/media"
	"github.com/aniketnotcoder/theogbot/internal/storage"
	"github.com/gotd/td/tg"
)

// retryFailedUploads retries only media uploads that failed during the first
// pass. It re-fetches only those source messages at the end, so failed files
// never accumulate on Railway's temporary disk while the main batch continues.
func (b *Bot) retryFailedUploads(
	ctx context.Context,
	api *tg.Client,
	destinationPeer tg.InputPeerClass,
	dest destination,
	label string,
	destinationTopicID int,
	tasks []retryTask,
	progress *progressReporter,
	chatID int64,
	done, failed, skipped int,
) (int, []string) {
	var succeeded int
	var refs []string

	for _, task := range tasks {
		if ctx.Err() != nil {
			break
		}

		downloaded := make([]*media.Downloaded, 0, len(task.elements))
		var retryErr error
		for j, elem := range task.elements {
			if progress != nil {
				progress.update(ctx, "Retrying failed download", task.firstIndex+j, done+succeeded, failed-succeeded, skipped, true)
			}
			item, err := media.DownloadMessageWithProgress(ctx, api, elem, b.tempDir, func(current, totalBytes int64) {
				if progress != nil {
					progress.updateBytes(ctx, "Retrying download", current, totalBytes, task.firstIndex+j, done+succeeded, failed-succeeded, skipped)
				}
			})
			if err != nil {
				retryErr = fmt.Errorf("redownload message %d: %w", task.ids[j], err)
				break
			}
			if task.album && item.Kind != "photo" && item.Kind != "video" {
				_ = item.Cleanup()
				retryErr = fmt.Errorf("album member %d is no longer a supported photo/video", task.ids[j])
				break
			}
			downloaded = append(downloaded, item)
		}

		if retryErr == nil {
			if task.album {
				items := make([]storage.AlbumItem, 0, len(downloaded))
				for _, item := range downloaded {
					items = append(items, storage.AlbumItem{
						Path: item.Path, Filename: item.Name, MIME: item.MIME,
						Kind: item.Kind, Caption: item.Caption,
						Attributes: item.Attributes, Entities: item.Entities,
					})
				}
				if progress != nil {
					progress.update(ctx, "Retrying failed album upload", task.firstIndex, done+succeeded, failed-succeeded, skipped, true)
				}
				retryProgress := func(current, totalBytes int64) {
					if progress != nil {
						progress.updateBytes(ctx, "Retrying upload", current, totalBytes, task.firstIndex, done+succeeded, failed-succeeded, skipped)
					}
				}
				if destinationTopicID > 0 {
					var updates tg.UpdatesClass
					updates, retryErr = b.storage.UploadAlbumToForumTopicWithProgress(ctx, destinationPeer, destinationTopicID, items, retryProgress)
					if retryErr == nil { refs = append(refs, destinationMessageRefs(dest, updates)...)}
				} else {
					var updates tg.UpdatesClass
					updates, retryErr = b.storage.UploadAlbumToWithProgress(ctx, destinationPeer, items, retryProgress)
					if retryErr == nil { refs = append(refs, destinationMessageRefs(dest, updates)...)}
				}
			} else if len(downloaded) == 1 {
				item := downloaded[0]
				if progress != nil {
					progress.update(ctx, "Retrying failed upload", task.firstIndex, done+succeeded, failed-succeeded, skipped, true)
				}
				retryProgress := func(current, totalBytes int64) {
					if progress != nil {
						progress.updateBytes(ctx, "Retrying upload", current, totalBytes, task.firstIndex, done+succeeded, failed-succeeded, skipped)
					}
				}
				var updates tg.UpdatesClass
				if destinationTopicID > 0 {
					updates, retryErr = b.storage.UploadPathToForumTopicWithProgress(ctx, destinationPeer, destinationTopicID, item.Path, item.Name, item.MIME, item.Caption, item.Kind, item.Attributes, item.Entities, retryProgress)
				} else {
					updates, retryErr = b.storage.UploadPathToWithProgress(ctx, destinationPeer, item.Path, item.Name, item.MIME, item.Caption, item.Kind, item.Attributes, item.Entities, retryProgress)
				}
				if retryErr == nil { refs = append(refs, destinationMessageRefs(dest, updates)...)}
			} else {
				retryErr = fmt.Errorf("invalid retry task containing %d media item(s)", len(downloaded))
			}
		}

		for _, item := range downloaded {
			if cleanupErr := item.Cleanup(); cleanupErr != nil {
				b.log.Warn("retry temporary file cleanup failed", "path", item.Path, "error", cleanupErr)
			}
		}

		if retryErr != nil {
			b.log.Warn("end-of-batch upload retry failed", "label", label, "message_ids", task.ids, "error", retryErr)
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s retry failed for message(s) %v: %v", label, task.ids, retryErr))
			continue
		}
		succeeded += len(task.elements)
		_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s retry succeeded for message(s) %v.", label, task.ids))
	}
	return succeeded, refs
}
