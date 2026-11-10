# TheOGBot

Go Telegram bot: Bot API for commands, gotd/td (MTProto) for each user's source account and a separate bot-authenticated storage uploader.

## Commands

- `/start`: open the image-backed welcome card and quick actions. Choose **Connect account**, reply with your phone number, then use the verification keypad and optional 2-step-password prompt.
- `/login +COUNTRYCODEPHONE`: start login directly; the bot guides you through reply prompts.
- `/code CODE` and `/password PASSWORD`: legacy login input commands remain available.
- `/batch LINK [LINK ...]`: transfer only the explicitly linked posts, in the supplied order. Links must belong to one source chat.
- `/copy CHAT_LINK [LIMIT]`: transfer accessible chat history. A post permalink can identify the chat; the trailing post ID is ignored in history mode. Default limit is 100, maximum 50000.
- `/cancel`: cancel your active transfer.
- `/createstorage`: create another private storage group with a custom name, automatically add and promote the uploader bot, and connect it. Send `default` to use “TheOGBot Storage”.
- `/setstorage`: choose an existing destination by forwarding a message from its group/channel in private chat.
- `/storage`: open storage management. Switch the active destination, create another named group, connect an existing group, or forget a saved entry (this does not delete the Telegram group).
- `/info`, `/help` or `/tutorial`, `/start`, `/logout`.
- `/backup`: send the raw bbolt database file to the configured administrator. Private chat only; sender ID must match `ADMIN_ID`.
- `/settutorial`: administrator-only setup command. Send it in private chat, then upload the tutorial video; the bot persists Telegram's `file_id` in bbolt and sends that video from `/help`, `/tutorial`, and the Help / Tutorial button.

## Required Railway variables

| Variable | Required | Description |
|---|---:|---|
| `BOT_TOKEN` | Yes | BotFather token. Used for Bot API commands and the separate MTProto uploader identity. |
| `API_ID` | Yes | Numeric API ID from https://my.telegram.org/apps. |
| `API_HASH` | Yes | API hash from https://my.telegram.org/apps. |
| `STORAGE_TARGET` | No | Optional shared fallback destination (private numeric ID). Prefer `/setstorage` for per-user destinations. |
| `TEMP_DIR` | No | Writable temporary directory; defaults to the OS temp directory. |
| `STORAGE_DB_PATH` | No | bbolt database path; defaults to `data/theogbot.db`. On Railway, mount a persistent volume at `/data` and set `/data/theogbot.db`. |
| `ADMIN_ID` | No | Telegram numeric user ID authorized to use `/backup`. Leave unset to keep administrator-only backup and tutorial-video management disabled. |

## Telegram setup

The welcome screen and tutorial use landscape SFW anime artwork; OTP and transfer progress cards use portrait artwork. Short prompts and notices stay text-only. Images come from the documented [Waifu API](https://docs.waifu.best/docs/getting-started/). No API key is required for random public SFW images. If the image service or Telegram photo delivery is unavailable, the bot falls back to a normal text message so actions remain usable.

1. Create a bot with @BotFather and set `BOT_TOKEN`.
2. Open TheOGBot and send `/start`. Use **Connect account** and complete the private login prompts.
3. Get `API_ID` and `API_HASH` from https://my.telegram.org/apps.
4. Choose **Create storage** or send `/createstorage`. The connected source account creates a private supergroup, invites the exact uploader bot configured by `BOT_TOKEN`, promotes it with limited admin rights, verifies that it can post, and creates a private invite link.
5. Tap **Join storage group** in the setup confirmation using the Telegram account you want to view your files with. This may be different from the source account connected in step 2. The bot saves the group as your personal destination.
6. To use an existing group/channel instead, add the uploader bot with posting permission, send `/setstorage`, and forward a message from that destination.
7. Keep credentials in Railway Variables, never in Git.

**Identity matters:** the connected source account creates the group and grants uploader access. The Telegram account chatting with TheOGBot must also join using the generated invite link; a private `t.me/c/...` message link does not grant membership.

## Transfer behavior and limitations

- Source access uses the user's own MTProto session; destination upload uses a separate bot-authenticated MTProto client.
- The transfer pipeline downloads supported media to temporary disk and uploads it as new messages. Storage persistence only saves destination metadata; it does not alter download, upload, forwarding, media conversion, batching, or progress code. Text-only messages are sent as text; photos are uploaded as photos; documents retain their filename, MIME type, video/audio/sticker attributes and caption text where Telegram exposes them. Consecutive photo/video posts from the same source album are uploaded as one destination media group (up to Telegram's 10-item album limit) when every selected member is available.
- Media downloads are capped at 2 GiB per item. Unsupported/service messages are reported as skipped rather than counted as uploads. Common caption formatting entities (bold, italic, underline, strike, code, links, mentions, spoilers, and blockquotes) are preserved. Source thumbnails and some less common entity types are not yet preserved. Album grouping is preserved for consecutive selected photos/videos; unsupported or incomplete album members fall back to individual transfers.
- Messages explicitly marked as protected are not downloaded. Telegram's API response is authoritative; the bot does not claim to override Telegram restrictions.
- Source Telegram login sessions remain in memory and must be reconnected after a restart. Storage groups and each user's active destination are persisted in bbolt at `STORAGE_DB_PATH`. On Railway, attach a persistent volume mounted at `/data` and set `STORAGE_DB_PATH=/data/theogbot.db`; without a persistent volume, the database file can still be lost on redeploy. Existing groups can be re-added through `/setstorage` if they were not present when persistence was first enabled.
- Temporary files use Railway's ephemeral filesystem. Ensure adequate free space for the largest file being transferred.
- Transfers are sequential per user. `/cancel` requests safe cancellation. A single edited progress message reports the current stage, item position, live downloaded/uploaded bytes, percentage where the total is known, measured transfer speed, byte ETA, elapsed time, and uploaded/failed/skipped totals. Download and upload use up to four chunks per file. Downloads require a 128 MiB free-space reserve and are capped at 2 GiB per item. gotd v0.163.0 handles safe MTProto connection-loss recovery by retrying requests that were sent but not acknowledged on a replacement connection. At the application layer, only explicit Telegram FloodWait responses are retried; uncertain final-send outcomes are never blindly replayed. Successful sends return destination message links where available, or chat/message identifiers for basic groups.

## Development

```sh
go mod download
go test ./...
go build ./cmd/theogbot
go run ./cmd/theogbot
```


## Railway deployment and testing

Attach a persistent Railway volume and mount it at `/data`. Set:

- `STORAGE_DB_PATH=/data/theogbot.db`
- `ADMIN_ID=<your Telegram numeric user ID>` to enable the private-chat `/backup` command.

The Docker entrypoint fixes ownership of the mounted `/data` volume for UID/GID `10001`, then drops privileges before starting the bot. The bbolt database is opened with file mode `0600`, creates its parent directory when needed, and uses a one-second lock timeout.

Storage groups and each user's active destination persist in bbolt across restarts when the volume remains attached. Source Telegram login sessions remain in memory and must be reconnected after a restart. Temporary media remains on ephemeral storage and is cleaned up after transfers.

For a safe live test, use a private test destination, add the configured uploader bot with posting rights, send `/setstorage` and forward a message from that destination, then run `/storage`. Log in with a test source account and transfer a small text/photo/video set with `/batch` or a small history with `/copy`. Confirm progress and destination results, then test `/cancel` on a larger permitted file. Do not test against a production channel until the private test succeeds.


## Telegram commands

- `/batch <chat-link> <start> to <end>` — copy an inclusive message-ID range from one chat (for example, `/batch https://t.me/c/4332971248 61 to 309`).
- `/batch <chat-link> <start>-<end>` — equivalent compact range syntax.
- `/batch <post-link> [<post-link> ...]` — existing explicit-post batch syntax.
- `/wistia <post-link> [stream|parallel]` — upload one accessible Telegram video post to the configured Wistia folder. `stream` (default) pipes Telegram download directly into Wistia without a media temp file; `parallel` uses gotd's parallel download into a temporary file before uploading.
- The Wistia upload command is also available from the private-chat menu. It requires a connected MTProto account and Wistia environment variables.

Wistia variables are optional and do not prevent normal Telegram-to-Telegram transfers from starting:
`WISTIA_API_TOKEN` (Upload media permission), `WISTIA_DATA_API_TOKEN` (read folder/media permission for readiness polling), and `WISTIA_PROJECT_ID` (destination folder ID). If the Data API token is omitted, the bot reports the Wistia upload result without polling processing status.
