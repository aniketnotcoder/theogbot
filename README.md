# TheOGBot

Go Telegram bot: Bot API for commands, gotd/td (MTProto) for each user's source account and a separate bot-authenticated storage uploader.

## Commands

- `/login +COUNTRYCODEPHONE`, then `/code CODE` and optionally `/password PASSWORD`: connect your own Telegram account.
- `/batch LINK [LINK ...]`: transfer only the explicitly linked posts, in the supplied order. Links must belong to one source chat.
- `/copy CHAT_LINK [LIMIT]`: transfer accessible chat history. A post permalink can identify the chat; the trailing post ID is ignored in history mode. Default limit is 50000, maximum 50000.
- `/cancel`: cancel your active transfer.
- `/storage`: check whether the separate uploader is connected.
- `/info`, `/help`, `/start`, `/logout`.

## Required Railway variables

| Variable | Required | Description |
|---|---:|---|
| `BOT_TOKEN` | Yes | BotFather token. Used for Bot API commands and the separate MTProto uploader identity. |
| `API_ID` | Yes | Numeric API ID from https://my.telegram.org/apps. |
| `API_HASH` | Yes | API hash from https://my.telegram.org/apps. |
| `STORAGE_TARGET` | Yes for transfers | Public username (e.g. `@my_storage_group`) or private numeric ID (e.g. `-1001234567890`). |
| `TEMP_DIR` | No | Writable temporary directory; defaults to the OS temp directory. |

## Telegram setup

1. Create a bot with @BotFather and set `BOT_TOKEN`.
2. Get `API_ID` and `API_HASH` from https://my.telegram.org/apps.
3. Create or choose a private storage group/channel.
4. Add the exact bot whose token is configured in `BOT_TOKEN` to that destination. Give it permission to send media (and post messages for channels).
5. Set `STORAGE_TARGET` to the destination's numeric ID or public username.
6. The source account that invokes transfers separately logs in with `/login` and must already be able to access the source chat.
7. Keep credentials in Railway Variables, never in Git.

**Identity matters:** the source user's membership does not grant the uploader bot access to the storage group. If another Telegram account created the storage group, that is fine; the configured uploader bot must still be added to it.

## Transfer behavior and limitations

- Source access uses the user's own MTProto session; destination upload uses a separate bot-authenticated MTProto client.
- The transfer pipeline downloads supported media to temporary disk and uploads it as a new message. Text-only messages are sent as text; photos are uploaded as photos; documents retain their filename, MIME type, video/audio/sticker attributes and caption text where Telegram exposes them.
- Media downloads are capped at 2 GiB per item. Unsupported/service messages are reported as skipped rather than counted as uploads. Caption entities, thumbnails and album grouping are not yet preserved.
- Messages explicitly marked as protected are not downloaded. Telegram's API response is authoritative; the bot does not claim to override Telegram restrictions.
- Sessions are memory-only and are lost when Railway restarts or redeploys; users must log in again.
- Temporary files use Railway's ephemeral filesystem. Ensure adequate free space for the largest file being transferred.
- Transfers are sequential per user. `/cancel` requests safe cancellation.

## Development

```sh
go mod download
go test ./...
go build ./cmd/theogbot
go run ./cmd/theogbot
```
