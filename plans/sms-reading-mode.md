# Plan: SMS Reading Mode

> Source PRD: [#196](https://github.com/backflow-labs/backflow/issues/196)

## Architectural decisions

Durable decisions that apply across all phases:

- **Routes**: Keep inbound SMS on `POST /webhooks/sms/inbound`; no new route is introduced.
- **Schema**: No database changes; reuse existing `tasks` persistence and reading completion events.
- **Key models**: Reuse `CreateTaskRequest`, `Task` with `task_mode = "read"`, and notifier `Event` reading fields (`TLDR`, `Tags`).
- **Authentication**: Keep Twilio signature validation and allowed-sender checks as the SMS gateway before any task creation.
- **Service boundaries**: Reuse the existing read-task pipeline rather than creating a second SMS-specific reading flow; shared task creation remains the source of truth for defaults, validation, and event emission.

---

## Phase 1: SMS Read Dispatch

**User stories**: 1, 2, 3, 4, 5, 6, 9, 13, 14, 16, 17, 20

### What to build

Add a narrow SMS command path for messages that begin with `Read` or `read`. The inbound SMS flow should extract the first URL, validate it against the same HTTPS-only rules as other read-mode entry points, create a real `read` task through shared task creation, and reply with `Reading <url>...`. Messages that do not match the read command should continue through the existing generic SMS task flow unchanged.

### Acceptance criteria

- [ ] `Read <https-url>` and `read <https-url>` create `read` tasks through the shared task-creation path.
- [ ] The immediate Twilio response for a valid SMS read command is `Reading <url>...`.
- [ ] Non-command SMS messages still create generic `auto` tasks with current behavior.

---

## Phase 2: Invalid Command Handling

**User stories**: 7, 8, 16, 17

### What to build

Harden the SMS read-command entry point so malformed commands fail safely. If a message starts with the read keyword but does not contain a valid HTTPS URL, Backflow should return a helpful TwiML error response and create no task.

### Acceptance criteria

- [ ] `Read` messages without a valid HTTPS URL do not create tasks.
- [ ] Invalid read commands return a user-facing SMS error response.
- [ ] HTTPS validation behavior matches the existing read-mode contract.

---

## Phase 3: Read-Aware Completion SMS

**User stories**: 10, 11, 12, 15, 18

### What to build

Update outbound SMS formatting so completed read tasks send a reading-focused summary using the event payload already emitted by the orchestrator. Read completions should contain the TLDR and tags only, while non-read tasks and other event types keep their existing messaging behavior.

### Acceptance criteria

- [ ] Completed read tasks send SMS replies containing the TLDR.
- [ ] Tags are included when present and omitted when empty.
- [ ] Non-read task notifications remain unchanged.

---

## Phase 4: Docs and Contract Cleanup

**User stories**: 19, 20

### What to build

Bring the SMS documentation back in sync with the implemented product. Document the supported `Read <url>` / `read <url>` grammar, the immediate confirmation response, and the read-specific completion SMS shape, while removing outdated claims about inbound SMS repo parsing and review shortcuts.

### Acceptance criteria

- [ ] SMS setup docs describe the supported read command grammar accurately.
- [ ] SMS setup docs describe the actual reply behavior for read tasks.
- [ ] Outdated inbound-SMS parsing claims are removed or revised.
