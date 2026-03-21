package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"
)

// Discord interaction types.
const (
	InteractionTypePing               = 1
	InteractionTypeApplicationCommand = 2
	InteractionTypeMessageComponent   = 3
	InteractionTypeModalSubmit        = 5
)

// Discord interaction response types.
const (
	ResponseTypePong                   = 1
	ResponseTypeDeferredChannelMessage = 5
)

// Interaction is the minimal Discord interaction payload needed for routing.
type Interaction struct {
	Type int             `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// InteractionResponse is sent back to Discord.
type InteractionResponse struct {
	Type int `json:"type"`
}

// InteractionHandler returns an http.HandlerFunc that verifies and routes
// Discord interaction webhook requests.
func InteractionHandler(publicKey ed25519.PublicKey) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		signature := r.Header.Get("X-Signature-Ed25519")
		timestamp := r.Header.Get("X-Signature-Timestamp")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if !verifySignature(publicKey, signature, timestamp, body) {
			http.Error(w, "invalid request signature", http.StatusUnauthorized)
			return
		}

		var interaction Interaction
		if err := json.Unmarshal(body, &interaction); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}

		switch interaction.Type {
		case InteractionTypePing:
			respondJSON(w, InteractionResponse{Type: ResponseTypePong})
		case InteractionTypeApplicationCommand, InteractionTypeMessageComponent, InteractionTypeModalSubmit:
			log.Info().Int("type", interaction.Type).Msg("discord: interaction received (stub)")
			respondJSON(w, InteractionResponse{Type: ResponseTypeDeferredChannelMessage})
		default:
			http.Error(w, "unknown interaction type", http.StatusBadRequest)
		}
	}
}

func verifySignature(publicKey ed25519.PublicKey, signatureHex, timestamp string, body []byte) bool {
	if signatureHex == "" || timestamp == "" {
		return false
	}
	sig, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}
	msg := append([]byte(timestamp), body...)
	return ed25519.Verify(publicKey, msg, sig)
}

func respondJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ParsePublicKey decodes a hex-encoded Ed25519 public key.
func ParsePublicKey(hexKey string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid key length: got %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}
