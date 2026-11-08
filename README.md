# TheOGBot

Go Telegram bot: Bot API for commands, gotd/td (MTProto) for each user's source account and a separate bot-authenticated storage uploader.

## Commands

- `/start`: open the image-backed welcome card and quick actions. Choose **Connect account**, reply with your phone number, then use the verification keypad and optional 2-step-password prompt.
- `/login +COUNTRYCODEPHONE`: start login directly; the bot guides you through reply prompts.
- `/code CODE` and `/password PASSWORD`: legacy login input commands remain available.
- `/batch LINK [LINK ...]`: transfer only the explicitly linked posts, in the supplied order. Links must belong to one source chat.
- `/copy CHAT_LINK [LIMIT]`: transfer accessible chat history. A post permalink can identify the chat; the trailing post ID is ignored in history mode. Default limit is 100, maximum 50000.
- `/cancel`: cancel your active transfer.
- `/createstorage`: create a private storage group with your connected Telegram account, automatically add and promote the uploader bot, and connect it.
- `/setstorage`: choose an existing destination by forwarding a message from its group/channel in private chat.
- `/storage`: show your saved destination and uploader access status.
- `/info`, `/help` or `/tutorial`, `/start`, `/logout`.

## Required Railway variables

| Variable | Required | Description |
|---|---:|---|
| `BOT_TOKEN` | Yes | BotFather token. Used for Bot API commands and the separate MTProto uploader identity. |
| `API_ID` | Yes | Numeric API ID from https://my.telegram.org/apps. |
| `API_HASH` | Yes | API hash from https://my.telegram.org/apps. |
| `STORAGE_TARGET` | No | Optional shared fallback destination (private numeric ID). Prefer `/setstorage` for per-user destinations. |
| `TEMP_DIR` | No | Writable temporary directory; defaults to the OS temp directory. |

## Telegram setup

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
- The transfer pipeline downloads supported media to temporary disk and uploads it as new messages. Text-only messages are sent as text; photos are uploaded as photos; documents retain their filename, MIME type, video/audio/sticker attributes and caption text where Telegram exposes them. Consecutive photo/video posts from the same source album are uploaded as one destination media group (up to Telegram's 10-item album limit) when every selected member is available.
- Media downloads are capped at 2 GiB per item. Unsupported/service messages are reported as skipped rather than counted as uploads. Common caption formatting entities (bold, italic, underline, strike, code, links, mentions, spoilers, and blockquotes) are preserved. Source thumbnails and some less common entity types are not yet preserved. Album grouping is preserved for consecutive selected photos/videos; unsupported or incomplete album members fall back to individual transfers.
- Messages explicitly marked as protected are not downloaded. Telegram's API response is authoritative; the bot does not claim to override Telegram restrictions.
- Source sessions and per-user destinations are memory-only and are lost when Railway restarts or redeploys; users must log in again and run `/createstorage` again or reconnect an existing destination with `/setstorage`.
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

No new Railway environment variables were introduced for Phase 4. Keep the existing variables:

- `BOT_TOKEN` — required; the BotFather token used by the Bot API controller and the separate MTProto destination uploader.
- `API_ID` — required positive integer from https://my.telegram.org/apps.
- `API_HASH` — required from https://my.telegram.org/apps.
- `STORAGE_TARGET` — optional. Leave it empty when each user will configure a destination with `/setstorage`; otherwise set a private numeric destination ID as a shared fallback. The exact bot from `BOT_TOKEN` must be a member/admin with posting rights.
- `TEMP_DIR` — optional; defaults to the OS temporary directory. If set, use a writable path such as `/tmp/theogbot`. Ensure available temporary disk space can accommodate the largest in-flight item plus the 128 MiB reserve.

After deploying the new main build, verify Railway uses the Dockerfile build and start command supplied by the repository (the Docker image entrypoint is `theogbot`). In Railway → service → Variables, retain the three required credentials, configure `STORAGE_TARGET` only if using a shared fallback, and optionally set `TEMP_DIR`. Do not add a new variable for progress or transfer limits; these are code constants.

For a safe live test, use a private test destination, add the configured bot with posting rights, send `/setstorage` and forward a message from that destination, then run `/storage`. Log in with a test source account and transfer a small text/photo/video set with `/batch` or a small history with `/copy`. Confirm byte progress, speed/ETA, destination links, final counts, and then test `/cancel` on a larger permitted file. Do not test against a production channel until the private test succeeds.

**Session persistence:** source-account sessions and per-user destination selections are memory-only. A Railway restart/redeploy clears them; users must run `/login` and `/setstorage` again. No persistent database or Railway volume is currently required by the code. Temporary media is ephemeral and is deleted after each transfer.
