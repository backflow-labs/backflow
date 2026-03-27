package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/backflow-labs/backflow/internal/store"
)

func bearerAuthMiddleware(s store.Store, expectedToken string) func(http.Handler) http.Handler {
	if expectedToken == "" && s == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := parseBearerToken(r.Header.Get("Authorization"))
			if expectedToken != "" {
				if !ok || token != expectedToken {
					writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			if s == nil {
				next.ServeHTTP(w, r)
				return
			}

			hasKeys, err := s.HasAPIKeys(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to check API key configuration")
				return
			}
			if !hasKeys {
				next.ServeHTTP(w, r)
				return
			}

			if !ok {
				writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
				return
			}

			keyHash := sha256.Sum256([]byte(token))
			apiKey, err := s.GetAPIKeyByHash(r.Context(), hex.EncodeToString(keyHash[:]))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
				return
			}
			if apiKey == nil || apiKey.Expired(time.Now().UTC()) {
				writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
				return
			}

			requiredScope, ok := requiredScopeForRequest(r)
			if !ok {
				writeError(w, http.StatusForbidden, "API key does not have permission for this route")
				return
			}
			if !apiKey.HasPermission(requiredScope) {
				writeError(w, http.StatusForbidden, "API key does not have permission for this route")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func AuthMiddleware(s store.Store, expectedToken string) func(http.Handler) http.Handler {
	return bearerAuthMiddleware(s, expectedToken)
}

func requiredScopeForRequest(r *http.Request) (string, bool) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/health":
		return "health:read", true
	case r.Method == http.MethodGet && r.URL.Path == "/debug/stats":
		return "stats:read", true
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks":
		return "tasks:read", true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks":
		return "tasks:write", true
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/") && !strings.HasSuffix(r.URL.Path, "/logs"):
		return "tasks:read", true
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/logs"):
		return "tasks:read", true
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/"):
		return "tasks:write", true
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/retry"):
		return "tasks:write", true
	default:
		return "", false
	}
}

func parseBearerToken(header string) (string, bool) {
	if header == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
