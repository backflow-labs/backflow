# HANDOFF.md

Ledger of cross-PR tradeoffs. Each entry: decision → consequence for downstream work.

## Duplicate-URL handling in `handleReadingCompletion`

- **Orchestrator treats the agent's `novelty_verdict` as advisory, not authoritative.** The reader agent runs `read-lookup.sh` during its turn, but LLMs occasionally skip the lookup step and return `novel` / `extends_existing` for a URL that already exists. The orchestrator now calls `store.GetReadingByURL` itself before embedding. If the URL is already stored and `task.Force` is false, the task fails with `"reading already exists for url ... (id=...); resubmit with force=true to overwrite"`. If `task.Force` is true, the existing row is overwritten via `UpsertReading`.
- **`CreateReading` vs `UpsertReading` split by `Force`.** Non-forced writes go through `CreateReading` (the pre-insert duplicate check guarantees no conflict; if a race beats us to it, the unique index on `url` surfaces the error). Forced writes continue through `UpsertReading`.
- **`GetReadingByURL` added to `Store`.** Selects all columns except `embedding`. The embedding vector is expensive to transport; if a future caller needs it, fetch by id or add a targeted accessor.
- **API still lacks a `force` wire field.** The Discord `/backflow read` command already accepts `force` (default false). REST callers cannot set `Force` until the create endpoint is extended.
