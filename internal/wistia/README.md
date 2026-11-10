# Wistia upload package (isolated feature)

This package is deliberately not wired into the Telegram bot command router or production startup. It does not modify the existing Telegram-to-Telegram transfer engine.

## Configuration

Set these variables only in a dedicated test environment:

```env
WISTIA_API_TOKEN=<token with Upload video permission>
WISTIA_DATA_API_TOKEN=<token with Read all folder and media data permission>
WISTIA_PROJECT_ID=ai8812jxdq
```

The folder ID above is the hashed ID from the Wistia folder URL. The Upload API still names this parameter `project_id`.

Do not commit real tokens, paste them into chat, or expose them in logs. The broad legacy token shown during initial setup should be revoked and replaced with narrowly scoped tokens.

## Engines

### Direct Telegram stream

`Client.UploadTelegramMessage` extracts a Telegram video document and uses gotd's `downloader.Builder.Stream` to write into the Wistia multipart part through a bounded `io.Pipe`. It does not create a complete temporary video file.

gotd documents that `Stream` is sequential and cannot use the parallel `Parallel` downloader mode. This is an intentional first implementation trade-off. The existing parallel disk-backed downloader remains untouched.

### Reliable file upload

`Client.UploadTelegramMessageFromFile` uses gotd's parallel `Parallel()` downloader with a bounded worker count, writes to a private temporary file, then streams that file to Wistia. It deletes the file only after a confirmed successful upload. If upload fails, it returns the retained temporary path for reconciliation or retry. `Client.UploadFile` can retry a retained file while preserving its original filename. `Client.UploadPath` is the convenience wrapper when the on-disk filename should be used.

Both engines share the same multipart request implementation, known content length, cancellation propagation, and result parsing.

## API behavior

- Upload: `POST https://upload.wistia.com/`
- Authentication: `Authorization: Bearer <upload token>`
- Multipart fields: `project_id`, `name`, and `file`
- Media status: `GET https://api.wistia.com/modern/medias/{hashed_id}`
- Data API authentication: Bearer token with read-all-folder-and-media-data permission
- API version header: `X-Wistia-API-Version: 2026-09`

A successful upload response means Wistia accepted the file; it does not mean encoding is complete. Call `WaitUntilReady` with a Data API token before treating the video as playable.

## Safety and limits

- No bot command or startup integration exists yet.
- No production environment variables are required.
- No changes are made to `cmd/theogbot`, `internal/bot`, `internal/media`, `internal/storage`, or bbolt schemas.
- Upload requests are not blindly retried. If the connection fails after the request may have reached Wistia, the caller must reconcile account media before retrying to avoid duplicate uploads.
- Streaming uses a known Telegram document size to set multipart Content-Length. Unknown-size sources are rejected.
- The package uses no additional Go module dependencies.

## Validation

```sh
go test ./internal/wistia
go test ./...
go test -race ./internal/wistia
go build ./cmd/theogbot
```

Real Telegram and Wistia integration tests require valid credentials and must be run against a private test video/folder, not as part of CI.
