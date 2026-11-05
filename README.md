# theogbot

A Go-based Telegram utility bot project.

## Status

Early MVP scaffold. The bot API layer and MTProto account-client layer are developed separately. User sessions are process-local for the initial MVP and are cleared on restart.

## Scope

- Telegram Bot API command interface
- Explicit user-account authentication through MTProto
- Retrieval of content the authenticated account can access
- Batch processing and progress reporting
- Copy/recreate supported, unprotected messages into the requesting user's bot chat
- No database or external object storage in the MVP

Protected-content restrictions, Telegram permissions, and rate limits are respected.

## Development

Requires Go 1.24 or newer and Telegram API credentials (`BOT_TOKEN`, `API_ID`, `API_HASH`). See `.env.example`.
