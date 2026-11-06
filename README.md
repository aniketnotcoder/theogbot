# TheOGBot

A Go Telegram bot with a long-running Bot API poller and per-user MTProto clients.

## Current MVP status

The repository currently contains the command interface and the initial MTProto login flow. Batch retrieval and history-copy handlers are not implemented yet; they are the next development milestone. Do not treat placeholder /batch or /copy replies as completed features.

## Architecture

- Go 1.25
- Telegram Bot API long polling
- MTProto via gotd/td
- Railway deployment using the included Dockerfile
- In-memory sessions only; restarting/redeploying logs users out
- No database and no external object storage
- Native MTProto forwarding for the transfer path; no media temp files needed for ordinary forwards

The bot must run as a persistent service. It is not designed for Vercel serverless functions.

## Railway deployment

1. Push this repository to GitHub.
2. In Railway, create a project and choose Deploy from GitHub repo.
3. Select aniketnotcoder/theogbot. Railway should detect the Dockerfile automatically.
4. Open the service's Variables tab and add the three variables below.
5. Deploy and inspect the deployment logs. A successful startup logs that the bot connected and long polling started.

### Required Railway variables

| Variable | Value |
|---|---|
| BOT_TOKEN | Bot token from @BotFather |
| API_ID | Numeric API ID from https://my.telegram.org/apps |
| API_HASH | API hash from https://my.telegram.org/apps |

Never commit real credentials or paste them into public issues. If a token has been exposed, revoke it and create a replacement.

## Commands

- /start, /help, /info
- /login +COUNTRYCODEPHONE
- /code YOUR_CODE
- /password YOUR_2FA_PASSWORD
- /logout
- /batch and /copy are planned and not yet functional.

For security, send login codes and 2FA passwords only in the private bot chat. The bot attempts to delete those incoming messages after receiving them, but Telegram deletion is not a guarantee that the message was never visible or retained elsewhere. Never use an account you do not own or control.

## Development

    go mod download
    go test ./...
    go run ./cmd/theogbot

## Forwarding behavior

Both `/batch` and `/copy` are intended to use the same native MTProto forwarding path. The engine batches up to 100 message IDs per `messages.forwardMessages` request and returns Telegram RPC errors as-is, including protected-content restrictions. It does not silently fall back to download/re-upload. The engine requires resolved source and destination `InputPeer` values; peer resolution and command integration are still under development.
