# HANDOFF.md

Ledger of cross-PR tradeoffs. Each entry: decision → consequence for downstream work.

## #199 — SMS read dispatch and task-creation consolidation

- **Canonical task-creation lives in `internal/taskcreate/`, not `internal/api/`.** REST, Discord, and SMS all call `taskcreate.NewTask` / `taskcreate.NewReadTask` directly. `taskcreate` takes `notify.Emitter` and emits `task.created` itself — emission is a structural invariant, not a caller obligation. If a future entry point creates tasks, route it through `taskcreate` too; don't reintroduce a wrapper that optionally emits.
- **`MessagingNotifier` lives in `internal/messaging/`, not `internal/notify/`.** Required to break the cycle `taskcreate → notify → messaging → taskcreate`. The rule going forward: transport-specific notifiers live alongside their transport. `notify/` stays transport-agnostic.
- **Empty-TLDR is a reading-task failure (fresh or forced).** Enforced in `handleReadingCompletion` after the duplicate short-circuit. Downstream SMS/Discord/webhook all see the same `task.failed` signal. The duplicate short-circuit is the only path where an empty TLDR is acceptable (the existing reading row carries the real content).
- **SMS read is HTTPS-only, first-URL-wins, no `force`.** `parseReadCommand` delegates URL validation to `discord.ValidateReadURL`. If a future change moves `ValidateReadURL` out of `internal/discord/` (it's a shared business rule, not a Discord concept), update the import in `internal/messaging/inbound.go`. A `force` flag for SMS read is deferred — add it by extending `parseReadCommand` to accept a trailing `force` keyword.

## Duplicate-URL handling for read-mode tasks

- **Duplicate check runs at dispatch, not completion.** Before the orchestrator launches a reader container for a `task_mode=read` task, it calls `store.GetReadingByURL(ctx, task.Prompt)`. If the URL already exists and `task.Force` is false, the task is marked `failed` with `"reading already exists for url ... (id=...); resubmit with force=true to overwrite"` and `task.failed` is emitted — no container, no embedding call, no spend. `Force=true` bypasses the check and dispatches normally, with `UpsertReading` overwriting the existing row on completion. The orchestrator is the source of truth for duplicate detection; the in-container `read-lookup.sh` remains as a best-effort agent hint but is advisory.
- **`GetReadingByURL` added to `Store`.** Selects all columns except `embedding`. The embedding vector is expensive to transport; if a future caller needs it, fetch by id or add a targeted accessor.
- **Completion path uses `UpsertReading` unconditionally, and `CreateReading` is removed from the `Store` interface entirely.** The dispatch-time guard covers non-forced duplicates; the only remaining completion-time write paths are `Force=true` (overwrite by design) and the rare concurrent-dispatch race where two read tasks pass their lookup before either writes (for which "upsert" is the benign outcome). The unique index on `readings.url` remains as a crash-rather-than-corrupt backstop.
- **API still lacks a `force` wire field.** The Discord `/backflow read` command already accepts `force` (default false). REST callers cannot set `Force` until the create endpoint is extended.
