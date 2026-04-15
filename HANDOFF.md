# Reading Completion Pipeline (#174) — Handoff Notes

## Context

This PR implements issue #174: the orchestrator-side pipeline for handling reader-task completion. Decisions that expand or contract the scope are recorded below so downstream issues (#175, #177) don't re-litigate them.

## Decisions made in this PR

### 1. `Task.Force` added here (unblocks #175)

The acceptance criteria call for `UpsertReading` when `task.Force == true`, but the `Force` field did not exist on the `Task` model. We added:

- `Force bool` on `models.Task`
- `force BOOLEAN NOT NULL DEFAULT false` column on the `tasks` table via a new goose migration
- Updates to `postgres.go` scan/insert

**Consequence for #175 (REST API reading task creation):** the backend now persists `Force`. #175 only needs to wire `Force *bool` onto `CreateTaskRequest` and copy it into the `Task` at creation time — no schema work required.

**Alternative considered:** reading force from a field the reader agent writes to `status.json`. Rejected because it couples force semantics to agent output rather than caller intent, and because force affects the agent's own duplicate-check behavior (it needs to know up-front).

### 2. `Event.TaskMode` added here (unblocks #177)

`NewEvent` did not previously copy `task.TaskMode` onto the `Event`. We added `TaskMode` to the `Event` struct and populate it in `NewEvent`. This costs one line and lets `DiscordNotifier` branch on `event.TaskMode == "read"` in #177 without further plumbing.

**Consequence for #177 (Discord reading completion embed):** `event.TaskMode` is guaranteed set. The new reading fields on `Event` (`TLDR`, `NoveltyVerdict`, `Tags`, `Connections`) are also populated for reading-task completion events. #177 is now purely a formatting change inside `DiscordNotifier`.

### 3. Embeddings client is single-shot (no retries)

`OpenAIEmbedder.Embed` makes one HTTP call and returns the error. Rationale:

- Matches the orchestrator's existing pattern of fail-fast external calls.
- Failures surface as task failures, which the higher-level task retry system can handle.
- Keeps the client trivially testable and easy to reason about.

**Consequence for future work:** if transient OpenAI 429/5xx rates become a problem, retries can be added inside the client without changing the interface. The `Embedder` interface (`Embed(ctx, text) ([]float32, error)`) is deliberately narrow.

### 4. `RawOutput` is the marshaled `AgentStatus`

Rather than adding a separate `raw_output` field that the agent writes, the orchestrator marshals the full parsed `AgentStatus` to `json.RawMessage` when constructing the `Reading`. This is lossless for typed fields, avoids a round-trip through the agent image, and lets the agent prompt evolve without adding new schema fields.

**Consequence:** if the agent starts emitting fields we don't parse yet, they won't be captured in `raw_output`. Acceptable trade-off — `status.json` is the agent's public interface, and extending `AgentStatus` to pick up a new field is a two-line change.

## Things we did NOT do (scope kept narrow)

### Discord reading embed formatting — deferred to #177

`DiscordNotifier` still uses the existing code/review embed shape for reading completions. The data is present on the event; #177 adds the reading-specific format.

### API-level `force` wiring — deferred to #175

`CreateTaskRequest` has no `force` field yet. Submitting `{"force": true}` today has no effect. #175 adds the API input and the assignment `task.Force = *req.Force` at creation time.

### Reading cancellation / interruption semantics — deferred

Reading tasks use the same cancellation / interruption flow as code tasks. No reading row is written until completion, so interrupted reading tasks leave no orphans in the `readings` table. Intentionally simple; revisit only if partial readings become a feature request.

### `GetReadingByURL` / reading query methods — not added

`UpsertReading` handles the force-re-read case via `ON CONFLICT (url)`. No code path in this PR reads readings back from the DB, so adding accessors would be speculative. Add when a consumer appears.

### `SUPABASE_READER_KEY` config — not added

The PRD mentions a custom `backflow_reader` JWT. The repo currently uses `SUPABASE_ANON_KEY`. Aligning the two is a separate Supabase-side task (role + JWT minting) and has no bearing on the completion pipeline.

## Test coverage

All new code is covered by unit tests with mocked dependencies:

- `internal/embeddings/` — `httptest.NewServer` stubs OpenAI, verifies request shape, response parsing, and error handling (`TestOpenAIEmbedder_PostsCorrectPayload`, `_ParsesEmbedding`, `_ReturnsErrorOnNon200`, `_EmptyData`).
- `internal/orchestrator/monitor_test.go` — mocked store + mocked embedder verifies success, force upsert, embed failure, and store failure paths for reading completion (`TestHandleCompletion_ReadSuccess_EmbedsAndCreatesReading`, `_ReadForce_CallsUpsertReading`, `_ReadEmbedFailure_MarksTaskFailed`, `_ReadStoreFailure_MarksTaskFailed`).
- `internal/orchestrator/runner_test.go` — JSON round-trip for the new `AgentStatus` reading fields.
- `internal/store/postgres_test.go` — `Force` column round-trip against real pgvector Postgres via testcontainers.
- `internal/notify/bus_test.go` — `WithReading` option populates reading fields; `NewEvent` copies `TaskMode` from task.

End-to-end validation against real OpenAI + Supabase + Fargate is out of scope per the PRD's "explicitly not tested" list. Run a real reader task post-deploy to validate the full chain.

## Pre-existing flake

`TestFakeAgentTimeout` in `test/blackbox/fake-agent/fake_agent_test.go` fails on `main` too — the timeout outcome's container sometimes exits 0 before the 2-second sleep in the test. Unrelated to this work; left alone.
