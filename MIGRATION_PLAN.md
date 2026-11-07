# TheOGBot migration plan — Auto-Forward-Bot-V2 reference

## Research findings
Reference: https://github.com/devgaganin/Auto-Forward-Bot-V2
Inspected bot.py, main.py, config.py, database.py and plugins/public.py, test.py, regix.py, settings.py, utils.py.

### What the reference actually does
1. Two-layer design: the public Bot API bot handles commands; a separately configured transfer client is created per user from either a personal user session or an additional bot token.
2. The transfer client is configurable. The code starts that client, checks source access with get_messages, and tests destination posting by sending then deleting a test message.
3. Destination is configured, not auto-created. Users add a destination by forwarding a message from it; chat ID/title are stored per user. There is no automatic storage-supergroup creation flow in the inspected source.
4. Public and private t.me/c/<internal-id>/<message-id> links are parsed. Private IDs are converted to Telegram's -100... form.
5. Copy vs forward: with forward_tag enabled it uses forward_messages; otherwise it creates new messages. For media with captions it tries send_cached_media using the media file_id; other messages use copy_message. This is not a universal local download-to-disk pipeline.
6. Progress/cancellation uses per-user locks, cancel flags, counts, ETA and edited status messages. FloodWait retries exist in selected send paths.
7. MongoDB stores users, transfer-client credentials, destinations and preferences. TheOGBot can start without a DB, but its in-memory sessions are lost on Railway restart.
8. Treat this repository as a behavioral reference, not production-quality code to copy line-for-line. Some iteration and error-handling paths need independent validation.

## Current TheOGBot gaps found
- /batch parses explicit links but currently scans history to find requested IDs; it should fetch exact IDs directly and preserve supplied order.
- /copy intentionally ignores a trailing post ID and copies whole-chat history, defaulting to 5000. The command needs explicit and documented range/order semantics.
- History iteration needs an audit for pagination direction, gaps, oldest/newest ordering, exact limits and early RPC errors.
- /storage resolves the configured numeric ID from the storage bot's dialogs. A group created by another account works only after the actual uploader bot joins it and Telegram exposes it in that bot's dialogs. Source-account membership does not grant bot membership.
- Current upload path sends everything as a document. Captions, entities, media attributes, thumbnails, albums, text-only messages and destination message links are incomplete.
- Storage readiness must return promptly on startup failure rather than leaving commands waiting until their context deadline.
- Fix existing CI compile errors before feature work. The reported failure in internal/forward/peers.go was undefined time.

## Target architecture
- Control plane: existing Bot API bot handles commands, login prompts, configuration, progress and cancellation.
- Source identity: each user authenticates a personal MTProto account. It resolves source chats and reads messages accessible to that account.
- Destination identity: a separately configured uploader posts into the user's selected destination. Initially use the existing Bot-token MTProto uploader and require that exact bot to be a member/admin with posting rights. Add optional per-user destination user sessions only if required; never silently substitute identities.
- Per-user configuration: source session, destination chat, transfer preferences and active job. Keep secrets out of logs and git.
- Transfer engine: classify each message, use supported server-side copy/reference methods where appropriate, otherwise download to bounded temporary disk and upload a new message. Record exact Telegram RPC errors.
- Results: retain destination peer/message IDs and return valid links where Telegram exposes them.

## Command contract
### /batch LINK [LINK ...]
- Processes only explicitly supplied message links, in exact input order.
- Each link identifies one post, including t.me/c links.
- Initially require all links to belong to the same source chat; reject mixed chats clearly.
- Never reinterpret a missing post ID as a history request.

### /copy CHAT_LINK [options]
- Copies chat history, not a single post.
- A post permalink may identify the chat; explain that its trailing post ID is ignored for history mode.
- Define ordering and range options explicitly: limit, from_id, to_id or latest. If history is fetched newest-first, reverse it before uploading when chronological order is desired.
- Report actual discovered count and distinguish inaccessible/deleted gaps from API failures. Avoid surprising implicit scans.

## Phased implementation and acceptance gates
### Phase 0 — stabilize
- Fix CI compile/test failures first.
- Add tests for private links, /batch versus /copy parsing, pagination, ordering and argument validation.
- Make storage startup bounded and /storage diagnostics actionable.
Gate: CI green; login/logout/status stable; storage readiness returns promptly.

### Phase 1 — identity and destination configuration
- Model per-user transfer configuration: destination peer, uploader identity and source/destination access state.
- Keep control-plane bot identity separate from MTProto transfer identity.
- Add a setup flow that accepts a forwarded message from the destination and resolves the chat using the actual uploader identity.
- Report authenticated uploader identity, destination title/ID and posting permission.
Gate: a group created by a different account works after the uploader bot is added with posting rights; otherwise /storage identifies exactly which identity lacks access.

### Phase 2 — message transfer semantics
- Classify text, photo, document, video, audio, animation, sticker, voice, poll, album and service messages.
- Use supported copy/reference methods when available; use download-upload for supported media when needed.
- Preserve captions, entities, filenames, MIME, dimensions, duration, streaming attributes and thumbnails where available.
- Handle text-only messages without attempting a file download.
- Report unsupported, deleted or protected messages distinctly; never count skips as uploads.
Gate: mixed-media test matrix with verified destination message IDs and per-item outcomes.

### Phase 3 — correct commands
- /batch fetches exact IDs, resolves the source peer once and preserves supplied order.
- /copy gets a dedicated paginated history iterator with correct offsets, bounded limits, gap tolerance, deterministic ordering and a resume cursor.
- Add /cancel, per-user job locks and protection against overlapping jobs for one destination.
Gate: test history containing ID gaps and mixed media yields exactly the expected ordered set; batch never silently becomes copy-history.

### Phase 4 — progress and reliability
- One editable status message shows stage, current item, bytes, percentage, speed, ETA, uploaded/failed/skipped and total.
- Add byte-level download/upload callbacks and throttle edits.
- Retry FloodWait only for safe operations; avoid duplicate uploads after uncertain send outcomes.
- Bound workers, file sizes and timeouts; check disk space; clean temporary files on cancel/failure.
Gate: cancel during download/upload; no orphan temp files or duplicate uploads; counts reconcile.

### Phase 5 — Railway readiness
- Decide whether sessions must survive restarts. If persistent, encrypt session material and restrict access.
- Verify temporary disk capacity, cleanup and graceful shutdown.
- Test on a non-production Railway service/branch and record file size, duration, throughput, memory and RPC errors.
- Keep production on main; do not merge or change Railway deployment settings without explicit approval.
Gate: repeatable end-to-end tests and user approval before rollout.

## Safeguards
- Do not copy hardcoded credentials, MongoDB URIs or session-handling practices from the reference.
- Never log login codes, passwords or session strings.
- Do not claim universal restricted-content support from README marketing text; test exact message/API behavior.
- No automatic destination-group creation in the first release. Configure a destination accessible to the chosen uploader identity.