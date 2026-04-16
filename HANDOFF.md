# HANDOFF.md

Ledger of cross-PR tradeoffs. Each entry: decision → consequence for downstream work.

## #199 — SMS read dispatch and task-creation consolidation

- **Canonical task-creation lives in `internal/taskcreate/`, not `internal/api/`.** REST, Discord, and SMS all call `taskcreate.NewTask` / `taskcreate.NewReadTask` directly. `taskcreate` takes `notify.Emitter` and emits `task.created` itself — emission is a structural invariant, not a caller obligation. If a future entry point creates tasks, route it through `taskcreate` too; don't reintroduce a wrapper that optionally emits.
- **`MessagingNotifier` lives in `internal/messaging/`, not `internal/notify/`.** Required to break the cycle `taskcreate → notify → messaging → taskcreate`. The rule going forward: transport-specific notifiers live alongside their transport. `notify/` stays transport-agnostic.
- **Empty-TLDR is a reading-task failure (fresh or forced).** Enforced in `handleReadingCompletion` after the duplicate short-circuit. Downstream SMS/Discord/webhook all see the same `task.failed` signal. The duplicate short-circuit is the only path where an empty TLDR is acceptable (the existing reading row carries the real content).
- **SMS read is HTTPS-only, first-URL-wins, no `force`.** `parseReadCommand` delegates URL validation to `discord.ValidateReadURL`. If a future change moves `ValidateReadURL` out of `internal/discord/` (it's a shared business rule, not a Discord concept), update the import in `internal/messaging/inbound.go`. A `force` flag for SMS read is deferred — add it by extending `parseReadCommand` to accept a trailing `force` keyword.
