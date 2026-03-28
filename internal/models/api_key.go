package models

import "time"

// APIKey represents a persisted bearer token for API access.
// The raw token is never stored; KeyHash is the SHA-256 hash used for lookup.
type APIKey struct {
	KeyHash     string     `json:"-"`
	Name        string     `json:"name"`
	Permissions []string   `json:"permissions"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (k *APIKey) HasPermission(scope string) bool {
	for _, perm := range k.Permissions {
		if perm == scope {
			return true
		}
	}
	return false
}

func (k *APIKey) Expired(now time.Time) bool {
	return k.ExpiresAt != nil && now.After(*k.ExpiresAt)
}
