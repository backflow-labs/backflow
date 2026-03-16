# SMS Setup (Twilio)

Backflow supports bidirectional SMS: create tasks by texting a phone number, and receive status notifications when tasks complete or fail.

## 1. Twilio Account Setup

1. Create a Twilio account at twilio.com
2. From the Twilio Console, grab your **Account SID** and **Auth Token**
3. Buy a phone number (or use the trial number) — note it in E.164 format (e.g. `+15551234567`)

## 2. Configure Backflow Environment Variables

Add these to your `.env`:

```bash
BACKFLOW_SMS_PROVIDER=twilio
TWILIO_ACCOUNT_SID=ACxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
TWILIO_AUTH_TOKEN=your_auth_token_here
BACKFLOW_SMS_FROM_NUMBER=+15551234567
# Optional: which events trigger SMS (defaults to task.completed,task.failed)
BACKFLOW_SMS_EVENTS=task.completed,task.failed
```

If `BACKFLOW_SMS_PROVIDER` is unset or empty, SMS is fully disabled (noop).

## 3. Register Allowed Senders

Inbound SMS (creating tasks via text) requires pre-authorized senders in the `allowed_senders` table. Insert rows directly in SQLite:

```sql
INSERT INTO allowed_senders (channel_type, address, default_repo, enabled, created_at)
VALUES ('sms', '+15559876543', 'https://github.com/org/repo', 1, datetime('now'));
```

- `address` — the sender's phone number in E.164 format
- `default_repo` — optional; used when the SMS body doesn't include a repo URL
- `enabled` — set to `0` to revoke access without deleting

## 4. Configure Twilio Inbound Webhook

In the Twilio Console, set the webhook URL for your phone number's **"A Message Comes In"** setting to:

```
https://your-backflow-host/webhooks/sms/inbound   (POST)
```

This endpoint receives incoming texts, authorizes the sender, parses the message for a repo URL and prompt, and creates a task.

## 5. How It Works

**Inbound (SMS to Task):** Text your Backflow number with a message like:

- `Fix the login bug` — uses sender's default repo
- `github.com/org/repo Fix the login bug` — explicit repo

The task is created with a `reply_channel` of `sms:+15559876543` so results go back to you.

**Outbound (Task to SMS):** When a task reaches a matching event (e.g. `task.completed`), Backflow sends an SMS to the reply channel:

- "Task bf_xxx completed. PR: https://github.com/org/repo/pull/42"
- "Task bf_xxx failed. Some error message"

## 6. Deployment Notes

- Twilio webhooks require a **publicly reachable URL** — use a tunnel (ngrok) for local dev
- The Twilio integration uses raw HTTP (no SDK dependency), with 3 retries and exponential backoff
- `max_subscription` auth mode runs one agent at a time, so inbound SMS tasks queue up serially
- `max_subscription` is not supported in `fargate` mode
