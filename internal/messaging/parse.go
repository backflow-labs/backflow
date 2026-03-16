package messaging

import (
	"fmt"
	"strings"
)

// ParseTaskFromSMS extracts a repo URL and prompt from an SMS body.
//
// Format: "<repo_url> <prompt>" or just "<prompt>" when defaultRepo is set.
// The first token is treated as a repo URL if it contains "/" or starts with
// "http://"/"https://".
func ParseTaskFromSMS(body, defaultRepo string) (repoURL, prompt string, err error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", "", fmt.Errorf("empty message")
	}

	parts := strings.SplitN(body, " ", 2)
	first := parts[0]

	if looksLikeURL(first) {
		repoURL = normalizeRepoURL(first)
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return "", "", fmt.Errorf("prompt is required after repo URL")
		}
		prompt = strings.TrimSpace(parts[1])
		return repoURL, prompt, nil
	}

	// No URL detected — use default repo
	if defaultRepo == "" {
		return "", "", fmt.Errorf("no repo URL found and no default repo configured for this sender")
	}
	return defaultRepo, body, nil
}

func looksLikeURL(s string) bool {
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	// Require domain/path pattern: the part before the first "/" must contain
	// a "." (e.g. "github.com/org/repo"). This avoids false positives like
	// "Fix/update" or "refactor/api.handler".
	slashIdx := strings.Index(s, "/")
	if slashIdx > 0 && strings.Contains(s[:slashIdx], ".") {
		return true
	}
	return false
}

func normalizeRepoURL(s string) string {
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return "https://" + s
	}
	return s
}
