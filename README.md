# TheOGBot

Go Telegram bot using the Telegram Bot API for commands and gotd/td (MTProto) for each user's source account and the storage uploader.

## Transfer flow

- `/batch LINK [LINK ...]`: fetches the selected messages from one source chat, downloads supported media using the logged-in user's MTProto session, then uploads each file as a new message to the configured storage chat using the bot's MTProto session.
- `/copy CHAT_LINK [LIMIT]`: reads accessible chat history (default 5000, max 50000) and uses the same download → upload pipeline.
- The destination is not the source user's Saved Messages and is not the bot's private chat. It is the separate storage group/channel configured by `STORAGE_TARGET`.
- Telegram-protected messages (`noforwards`) are not downloaded. Unsupported/text-only/service messages are reported as skipped. Telegram remains authoritative about whether a media file can be retrieved.
- Temporary files are removed after each upload. Transfers run sequentially to limit memory and disk use.
- Current progress is message-level; per-file byte percentage/speed, cancellation command, retries, and stored-message permalink reporting are not implemented yet.

## Required environment variables

| Variable | Required | Description |
|---|---:|---|
| `BOT_TOKEN` | Yes | Token from @BotFather. Used for bot commands and the storage uploader. |
| `API_ID` | Yes | Numeric MTProto API ID from https://my.telegram.org/apps. |
| `API_HASH` | Yes | MTProto API hash from https://my.telegram.org/apps. |
| `STORAGE_TARGET` | Yes for transfers | Destination username (e.g. `@my_storage_group`) or private numeric chat ID (e.g. `-1001234567890`). |
| `TEMP_DIR` | No | Temporary download directory. Defaults to the OS temp directory (usually `/tmp`). |

### Telegram setup

1. Create the bot with @BotFather and set `BOT_TOKEN`.
2. Get `API_ID` and `API_HASH` from https://my.telegram.org/apps.
3. Create a separate private storage supergroup/channel.
4. Add the bot to the storage destination. For a channel, grant permission to post; for a group, ensure it can send media.
5. Set `STORAGE_TARGET`. For a public destination use its username. For a private destination use its numeric Telegram chat ID, normally beginning with `-100` for supergroups/channels. The bot must already have access to that destination.
6. Keep all credentials in Railway Variables, never in Git.

The user invoking `/batch` or `/copy` must separately connect their own Telegram account using `/login`, then submit `/code` and, if requested, `/password` in a private chat with the bot. The source account must already be a member of the source chat.

## Railway deployment

- Deploy as a persistent Railway service from this repository's Dockerfile (not a serverless function).
- Set the variables above in **Service → Variables**.
- No database or object storage is required. User MTProto sessions are currently memory-only, so a restart/redeploy logs users out and they must log in again.
- `/tmp` is ephemeral. A Railway volume is not required for the current temporary-file workflow; ensure sufficient ephemeral disk space for the largest file being transferred.
- Monitor deployment logs for bot and storage MTProto connection errors.

## Commands

- `/start`, `/help`, `/info`, `/storage`
- `/login +COUNTRYCODEPHONE`
- `/code YOUR_CODE`
- `/password YOUR_2FA_PASSWORD`
- `/logout`
- `/batch LINK [LINK ...]`
- `/copy CHAT_LINK [LIMIT]`

Send login codes and 2FA passwords only in the private bot chat. The bot attempts to delete those incoming messages, but deletion is not a guarantee that they were never visible or retained elsewhere. Never use an account you do not own or control.

## Development

```sh
go mod download
go test ./...
go build ./cmd/theogbot
go run ./cmd/theogbot
```
