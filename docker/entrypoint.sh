#!/usr/bin/env bash
set -euo pipefail

# --- Configuration from environment ---
HARNESS="${HARNESS:-claude}"
REPO_URL="${REPO_URL:?REPO_URL is required}"
PROMPT="${PROMPT:?PROMPT is required}"
AUTH_MODE="${AUTH_MODE:-api_key}"
BRANCH="${BRANCH:-backflow/${TASK_ID:-$(date +%s)}}"
TARGET_BRANCH="${TARGET_BRANCH:-main}"
MODEL="${MODEL:-claude-sonnet-4-6}"
EFFORT="${EFFORT:-high}"
MAX_BUDGET_USD="${MAX_BUDGET_USD:-10}"
MAX_TURNS="${MAX_TURNS:-200}"
CREATE_PR="${CREATE_PR:-false}"
PR_TITLE="${PR_TITLE:-}"
PR_BODY="${PR_BODY:-}"
CLAUDE_MD="${CLAUDE_MD:-}"
TASK_CONTEXT="${TASK_CONTEXT:-}"
MAX_RETRIES="${MAX_RETRIES:-3}"

WORKSPACE="/home/agent/workspace"
STATUS_FILE="${WORKSPACE}/status.json"

write_status() {
    local exit_code="$1"
    local complete="$2"
    local needs_input="$3"
    local question="$4"
    local error_msg="$5"

    cat > "$STATUS_FILE" <<STATUSEOF
{
  "exit_code": ${exit_code},
  "complete": ${complete},
  "needs_input": ${needs_input},
  "question": $(echo "$question" | jq -R .),
  "error": $(echo "$error_msg" | jq -R .)
}
STATUSEOF
}

# --- Clone ---
echo "==> Cloning ${REPO_URL} (depth 50)..."
git clone --depth 50 "$REPO_URL" "$WORKSPACE"
cd "$WORKSPACE"

# Checkout target branch if it's not the default
git fetch origin "$TARGET_BRANCH" 2>/dev/null || true
git checkout "$TARGET_BRANCH" 2>/dev/null || true

# Create working branch
git checkout -b "$BRANCH"
echo "==> Working on branch: ${BRANCH}"

# --- Inject CLAUDE.md ---
if [ -n "$CLAUDE_MD" ]; then
    echo "==> Injecting CLAUDE.md content..."
    if [ -f CLAUDE.md ]; then
        echo "" >> CLAUDE.md
        echo "$CLAUDE_MD" >> CLAUDE.md
    else
        echo "$CLAUDE_MD" > CLAUDE.md
    fi
fi

# --- Build prompt ---
FULL_PROMPT="$PROMPT"
if [ -n "$TASK_CONTEXT" ]; then
    FULL_PROMPT="Context: ${TASK_CONTEXT}

${FULL_PROMPT}"
fi

# --- GitHub auth ---
if [ -n "${GITHUB_TOKEN:-}" ]; then
    echo "$GITHUB_TOKEN" | gh auth login --with-token 2>/dev/null || true
    gh auth setup-git 2>/dev/null || true
fi

# --- Auth mode setup ---
echo "==> Harness: ${HARNESS}"
echo "==> Auth mode: ${AUTH_MODE}"
echo "==> Model: ${MODEL}, effort: ${EFFORT}"
if [ "$HARNESS" = "codex" ]; then
    if [ -z "${OPENAI_API_KEY:-}" ]; then
        echo "ERROR: OPENAI_API_KEY is required for codex harness" >&2
        exit 1
    fi
elif [ "$AUTH_MODE" = "api_key" ]; then
    if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
        echo "ERROR: ANTHROPIC_API_KEY is required in api_key mode" >&2
        exit 1
    fi
elif [ "$AUTH_MODE" = "max_subscription" ]; then
    if [ ! -d "$HOME/.claude" ]; then
        echo "ERROR: ~/.claude credentials not mounted (required for max_subscription mode)" >&2
        exit 1
    fi
    echo "==> Using Max subscription credentials from ~/.claude"
else
    echo "ERROR: Unknown AUTH_MODE: ${AUTH_MODE}" >&2
    exit 1
fi

# --- Build command args based on harness ---
build_claude_args() {
    local prompt="$1"
    AGENT_CMD="claude"
    AGENT_ARGS=(
        -p "$prompt"
        --dangerously-skip-permissions
        --model "$MODEL"
        --effort "$EFFORT"
        --max-turns "$MAX_TURNS"
        --output-format json
        --verbose
    )
    # --max-budget-usd only applies to API key mode (billed per token)
    if [ "$AUTH_MODE" = "api_key" ]; then
        AGENT_ARGS+=(--max-budget-usd "$MAX_BUDGET_USD")
    fi
}

build_codex_args() {
    local prompt="$1"
    AGENT_CMD="codex"
    AGENT_ARGS=(
        --model "$MODEL"
        --reasoning-effort "$EFFORT"
        --approval-mode full-auto
        --quiet
        "$prompt"
    )
}

build_agent_args() {
    local prompt="$1"
    if [ "$HARNESS" = "codex" ]; then
        build_codex_args "$prompt"
    else
        build_claude_args "$prompt"
    fi
}

build_agent_args "$FULL_PROMPT"

# --- Run agent with retries ---
ATTEMPT=0
AGENT_EXIT=1
AGENT_OUTPUT=""

while [ $ATTEMPT -lt "$MAX_RETRIES" ]; do
    ATTEMPT=$((ATTEMPT + 1))
    echo "==> Running ${HARNESS} (attempt ${ATTEMPT}/${MAX_RETRIES})..."

    set +e
    AGENT_OUTPUT=$("$AGENT_CMD" "${AGENT_ARGS[@]}" 2>&1)
    AGENT_EXIT=$?
    set -e

    if [ $AGENT_EXIT -eq 0 ]; then
        echo "==> ${HARNESS} completed successfully"
        break
    fi

    echo "==> ${HARNESS} exited with code ${AGENT_EXIT} (attempt ${ATTEMPT})"
    echo "$AGENT_OUTPUT" | tail -30

    if [ $ATTEMPT -lt "$MAX_RETRIES" ]; then
        # Add error context to prompt for retry
        ERROR_TAIL=$(echo "$AGENT_OUTPUT" | tail -20)
        FULL_PROMPT="Previous attempt failed with error:
${ERROR_TAIL}

Please try again:
${PROMPT}"
        build_agent_args "$FULL_PROMPT"
    fi
done

# --- Detect needs_input ---
NEEDS_INPUT=false
QUESTION=""

if [ "$HARNESS" = "codex" ]; then
    # Codex outputs plain text; check for question patterns
    if echo "$AGENT_OUTPUT" | grep -qi 'question\|decision\|should I\|which approach\|do you want\|please clarify'; then
        NEEDS_INPUT=true
        QUESTION=$(echo "$AGENT_OUTPUT" | tail -5)
    fi
else
    if echo "$AGENT_OUTPUT" | jq -e '.result' 2>/dev/null | grep -qi 'question\|decision\|should I\|which approach\|do you want\|please clarify'; then
        NEEDS_INPUT=true
        QUESTION=$(echo "$AGENT_OUTPUT" | jq -r '.result // empty' 2>/dev/null | tail -5)
    fi

    # If claude ran out of turns without completing
    if echo "$AGENT_OUTPUT" | jq -e '.is_error' 2>/dev/null | grep -q 'true'; then
        if echo "$AGENT_OUTPUT" | jq -r '.error_type // empty' 2>/dev/null | grep -q 'max_turns'; then
            NEEDS_INPUT=true
            QUESTION="Agent ran out of turns (${MAX_TURNS}) without completing the task"
        fi
    fi
fi

# --- Write status ---
COMPLETE=false
if [ $AGENT_EXIT -eq 0 ]; then
    COMPLETE=true
fi
ERROR_MSG=""
if [ $AGENT_EXIT -ne 0 ]; then
    ERROR_MSG=$(echo "$AGENT_OUTPUT" | tail -5)
fi
write_status "$AGENT_EXIT" "$COMPLETE" "$NEEDS_INPUT" "$QUESTION" "$ERROR_MSG"

# --- Commit remaining changes ---
echo "==> Committing any remaining changes..."
git add -A
if ! git diff --cached --quiet; then
    COMMIT_MSG="backflow: agent work on task ${TASK_ID:-unknown}

Automated commit by backflow agent.
Harness: ${HARNESS}
Model: ${MODEL}"
    if [ "$AUTH_MODE" = "api_key" ]; then
        COMMIT_MSG="${COMMIT_MSG}
Budget: \$${MAX_BUDGET_USD}"
    fi
    git commit -m "$COMMIT_MSG"
fi

# --- Push ---
echo "==> Pushing branch ${BRANCH}..."
git push origin "$BRANCH" --force-with-lease 2>/dev/null || git push origin "$BRANCH"

# --- Create PR ---
if [ "$CREATE_PR" = "true" ]; then
    echo "==> Creating pull request..."
    PR_TITLE_FINAL="${PR_TITLE:-[backflow] ${PROMPT:0:60}}"

    if [ -z "$PR_BODY" ]; then
        PR_BODY="## Automated PR by Backflow

**Task:** ${PROMPT}

**Harness:** ${HARNESS}
**Model:** ${MODEL}"
        if [ "$AUTH_MODE" = "api_key" ]; then
            PR_BODY="${PR_BODY}
**Budget:** \$${MAX_BUDGET_USD}"
        else
            PR_BODY="${PR_BODY}
**Auth:** Max subscription"
        fi
        PR_BODY="${PR_BODY}

---
*Created by [backflow](https://github.com/backflow-labs/backflow) agent*"
    fi

    PR_URL=$(gh pr create \
        --title "$PR_TITLE_FINAL" \
        --body "$PR_BODY" \
        --base "$TARGET_BRANCH" \
        --head "$BRANCH" 2>&1) || true

    if [ -n "$PR_URL" ]; then
        echo "==> PR created: ${PR_URL}"
    fi
fi

echo "==> Done (exit code: ${AGENT_EXIT})"
exit $AGENT_EXIT
