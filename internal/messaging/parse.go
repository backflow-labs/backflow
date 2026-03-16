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
	// Require domain/path pattern: the part before the first "/" must look
	// like a hostname (e.g. "github.com/org/repo"). Both sides of the dot
	// must contain a letter to avoid false positives like "v2.0/migration".
	// Also require at least two path segments (host/a/b) to filter out
	// patterns like "config.yaml/broken".
	slashIdx := strings.Index(s, "/")
	if slashIdx > 0 {
		host := lower[:slashIdx]
		path := s[slashIdx+1:]
		dotIdx := strings.LastIndex(host, ".")
		if dotIdx > 0 && dotIdx < len(host)-1 &&
			containsLetter(host[:dotIdx]) && containsLetter(host[dotIdx+1:]) &&
			strings.Contains(path, "/") {
			return true
		}
	}
	return false
}

func containsLetter(s string) bool {
	for _, c := range s {
		if c >= 'a' && c <= 'z' {
			return true
		}
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
