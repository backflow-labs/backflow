package messaging

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseTaskFromSMS extracts a repo URL and prompt from an SMS body.
//
// Format: "<repo_url> <prompt>" or just "<prompt>" when defaultRepo is set.
// A GitHub repo, issue, or PR URL can appear anywhere in the body and will be
// used to derive the repository URL automatically.
func ParseTaskFromSMS(body, defaultRepo string) (repoURL, prompt string, err error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", "", fmt.Errorf("empty message")
	}

	parts := strings.Fields(body)
	for i, part := range parts {
		if !looksLikeGitHubURL(part) {
			continue
		}

		repoURL, err = normalizeGitHubRepoURL(part)
		if err != nil {
			return "", "", err
		}

		promptParts := make([]string, 0, len(parts)-1)
		promptParts = append(promptParts, parts[:i]...)
		promptParts = append(promptParts, parts[i+1:]...)
		prompt = strings.TrimSpace(strings.Join(promptParts, " "))
		if prompt == "" {
			return "", "", fmt.Errorf("prompt is required when a GitHub URL is provided")
		}
		return repoURL, prompt, nil
	}

	// No URL detected — use default repo
	if defaultRepo == "" {
		return "", "", fmt.Errorf("no repo URL found and no default repo configured for this sender")
	}
	return defaultRepo, body, nil
}

func looksLikeGitHubURL(s string) bool {
	u, err := parseURLToken(s)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Host)
	return host == "github.com" || host == "www.github.com"
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

func normalizeGitHubRepoURL(s string) (string, error) {
	u, err := parseURLToken(s)
	if err != nil {
		return "", fmt.Errorf("invalid GitHub URL: %w", err)
	}
	if u.Host == "" || u.Path == "" {
		return "", fmt.Errorf("invalid GitHub URL: missing host or path")
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid GitHub URL: expected owner/repo path")
	}

	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/%s/%s", scheme, u.Host, parts[0], parts[1]), nil
}

func parseURLToken(s string) (*url.URL, error) {
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		s = "https://" + s
	}
	return url.Parse(s)
}
