package bot

import (
	"context"
	"fmt"

	"github.com/aniketnotcoder/theogbot/internal/storage"
	"github.com/gotd/td/tg"
)

// retryFailedUploads retries only uploads that failed during the first pass.
// It reuses the already-downloaded temporary files, avoiding a second
// Telegram download and keeping the retry count bounded to exactly one.
func (b *Bot) retryFailedUploads(
	ctx context.Context,
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
	for index, task := range tasks {
		if ctx.Err() != nil {
			for _, remaining := range tasks[index:] {
				for _, item := range remaining.items {
					if cleanupErr := item.Cleanup(); cleanupErr != nil {
						b.log.Warn("retry temp cleanup after cancellation failed", "path", item.Path, "error", cleanupErr)
					}
				}
			}
			break
		}

		var updates tg.UpdatesClass
		var uploadErr error
		if task.album {
			items := make([]storage.AlbumItem, 0, len(task.items))
			for _, item := range task.items {
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
				updates, uploadErr = b.storage.UploadAlbumToForumTopicWithProgress(ctx, destinationPeer, destinationTopicID, items, retryProgress)
			} else {
				updates, uploadErr = b.storage.UploadAlbumToWithProgress(ctx, destinationPeer, items, retryProgress)
			}
		} else if len(task.items) == 1 {
			item := task.items[0]
			if progress != nil {
				progress.update(ctx, "Retrying failed upload", task.firstIndex, done+succeeded, failed-succeeded, skipped, true)
			}
			retryProgress := func(current, totalBytes int64) {
				if progress != nil {
					progress.updateBytes(ctx, "Retrying upload", current, totalBytes, task.firstIndex, done+succeeded, failed-succeeded, skipped)
				}
			}
			if destinationTopicID > 0 {
				updates, uploadErr = b.storage.UploadPathToForumTopicWithProgress(ctx, destinationPeer, destinationTopicID, item.Path, item.Name, item.MIME, item.Caption, item.Kind, item.Attributes, item.Entities, retryProgress)
			} else {
				updates, uploadErr = b.storage.UploadPathToWithProgress(ctx, destinationPeer, item.Path, item.Name, item.MIME, item.Caption, item.Kind, item.Attributes, item.Entities, retryProgress)
			}
		} else {
			uploadErr = fmt.Errorf("invalid retry task with %d files", len(task.items))
		}

		if uploadErr != nil {
			b.log.Warn("end-of-batch upload retry failed", "label", label, "first_index", task.firstIndex, "error", uploadErr)
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s retry failed for original item %d: %v", label, task.firstIndex, uploadErr))
		} else {
			succeeded += len(task.items)
			refs = append(refs, destinationMessageRefs(dest, updates)...)
			_ = b.sendMessage(ctx, chatID, fmt.Sprintf("%s retry succeeded for original item %d (%d item(s)).", label, task.firstIndex, len(task.items)))
		}
		for _, item := range task.items {
			if cleanupErr := item.Cleanup(); cleanupErr != nil {
				b.log.Warn("retry temp cleanup failed", "path", item.Path, "error", cleanupErr)
			}
		}
	}
	return succeeded, refs
}

