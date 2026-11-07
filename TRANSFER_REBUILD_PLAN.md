# TheOGBot transfer rebuild

## Target architecture

- Source: each user authenticates their own Telegram account through MTProto. That account resolves source chats and retrieves messages/media it is permitted to access.
- Transfer: download supported media to bounded temporary storage, validate metadata and size, then upload it as a new Telegram message.
- Destination: a separate MTProto bot client authenticates with the existing BotFather token and posts into one configured private storage group/channel where the bot has posting rights.
- Result: return the new storage message link when Telegram exposes a usable link; otherwise return a storage chat/message identifier.

The destination bot session is distinct from every user's source session. Do not resolve the bot username as the destination through the user's personal MTProto client.

## Phases and acceptance gates

### Phase 1 — identity and per-user destination setup
- Keep Bot API control, personal MTProto source identity, and the separate uploader bot identity distinct.
- Add `/setstorage`: the user forwards a message from a group/channel in a private chat; resolve that destination through the uploader bot's own dialogs.
- Store the destination per user in memory and use it for that user's `/batch` and `/copy` jobs.
- `/storage` reports the user's selected destination and whether the uploader bot can see it.
- Require the uploader bot to be a member with posting rights; never infer access from the source user's membership.

**Gate:** two users can select different destination chats, and the uploader identity can resolve each selected destination before transfer.

### Phase 2 — message transfer semantics
- Use the repository's pinned gotd/td v0.130.0 API.
- Extract media metadata and download locations from supported message types.
- Preserve consecutive source album groups (photos/videos sharing Telegram grouped_id) as destination media groups of up to 10 items.
- Download to temporary disk with cancellation, size limits, cleanup, and byte-progress callbacks.
- Upload with gotd's MTProto uploader from the bot-authenticated client.
- Preserve filename, MIME type, video/audio attributes, and caption where available.
- Preserve supported caption formatting entities using Telegram UTF-16 offsets; safely skip unsupported entity types.
- Unit-test metadata mapping, safe temporary filenames, album grouping/count limits, UTF-16 caption offsets, size guards, and cleanup.
- Record Telegram retrieval errors accurately. Do not attempt to defeat Telegram content-protection controls.

**Gate:** a permitted test file can be downloaded by the source account and uploaded by the bot into the configured storage chat, with the resulting message identified.

### Phase 3 — /batch and /copy on the media pipeline
- Replace native forwarding as the primary path with download → upload.
- Keep /batch for explicit message links and /copy for paginated chat history.
- Preserve source order; report per-item success/failure and continue after recoverable item failures.
- Respect Telegram access and content restrictions.

**Gate:** multi-message test with text-only, photo, document, and video messages; correct order and no silent skips.

### Phase 4 — live progress and controls
- Show current item, stage, live bytes, percentage, measured transfer speed, byte ETA, elapsed time, and completed/failed/skipped counts in one edited progress message.
- Edit one progress message with throttling to avoid excessive API calls.
- Add cancellation and bounded queue/concurrency.

**Gate:** CI run #56 passed `go test ./...` and `go build ./cmd/theogbot`. Live Railway validation remains: progress accuracy during real transfers and cancellation cleanup.

### Phase 5 — reliability and performance
- Add flood-wait-aware retry for safe idempotent operations, bounded concurrency, disk-space checks, startup diagnostics, and structured logs. (Implementation complete; CI passed.)
- Test large permitted files and failure cases on Railway.
- Tune from measured timings; do not promise throughput without measurements.

**Gate:** repeatable Railway integration run with recorded file size, duration, average throughput, memory, and outcome. This is the current testing phase.

## Current baseline (2026-11-06)

The current branch uses the authenticated user MTProto account for source retrieval and an independent bot-authenticated MTProto uploader for destination posts. /batch retrieves exact supplied IDs through messages.getMessages / channels.getMessages and preserves input order. /copy uses paginated history. Per-user destination setup is available through /setstorage by forwarding a message from the destination; destination selections are currently memory-only. The uploader client now starts without a shared STORAGE_TARGET so per-user destination selection works on a clean deployment. Consecutive photo/video messages sharing Telegram grouped_id are uploaded as a single media group when all members can be downloaded. Common caption formatting entities are preserved using Telegram UTF-16 offsets. Transfer jobs now create and edit one progress message with stage, item position, elapsed time, estimated remaining time, and uploaded/failed/skipped totals. Successful text, media, and album sends extract returned destination message IDs and report public or private channel links; basic groups report chat/message identifiers. Completed work also reports result references if the user cancels a running transfer.

Latest feature-branch CI: successful Go CI run #56 at commit `015d4a9b21dc463b881d14afe69011f68de13265`.

## Deployment safety

This work is being developed on the dev/transfer-rebuild branch. Railway production should remain connected to main only; do not change the Railway production branch until explicitly approved. CI on main is unaffected by this branch push. A preview deployment may still occur if Railway has branch-specific deployments enabled.

## Operations

- Keep BOT_TOKEN, API_ID, API_HASH, phone numbers, login codes, and passwords out of git.
- The bot must have posting permission in the destination group/channel.
- Railway needs writable temporary disk and enough free space for in-flight files.
- No persistent database is required for the first implementation; user sessions remain in memory.
