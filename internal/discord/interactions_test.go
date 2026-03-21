package discord

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func signRequest(priv ed25519.PrivateKey, timestamp, body string) string {
	msg := []byte(timestamp + body)
	sig := ed25519.Sign(priv, msg)
	return hex.EncodeToString(sig)
}

func postInteraction(handler http.HandlerFunc, _ ed25519.PublicKey, priv ed25519.PrivateKey, body string) *httptest.ResponseRecorder {
	timestamp := "1234567890"
	sig := signRequest(priv, timestamp, body)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/discord", strings.NewReader(body))
	req.Header.Set("X-Signature-Ed25519", sig)
	req.Header.Set("X-Signature-Timestamp", timestamp)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestInteractionHandler_Ping(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub)

	rr := postInteraction(handler, pub, priv, `{"type":1}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp InteractionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != ResponseTypePong {
		t.Errorf("response type = %d, want %d (PONG)", resp.Type, ResponseTypePong)
	}
}

func TestInteractionHandler_InvalidSignature(t *testing.T) {
	pub, _ := testKeyPair(t)
	handler := InteractionHandler(pub)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/discord", strings.NewReader(`{"type":1}`))
	req.Header.Set("X-Signature-Ed25519", "deadbeef")
	req.Header.Set("X-Signature-Timestamp", "1234567890")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestInteractionHandler_MissingHeaders(t *testing.T) {
	pub, _ := testKeyPair(t)
	handler := InteractionHandler(pub)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/discord", strings.NewReader(`{"type":1}`))
	// No signature headers

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestInteractionHandler_ApplicationCommand(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub)

	rr := postInteraction(handler, pub, priv, `{"type":2,"data":{"name":"task"}}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	var resp InteractionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Type != ResponseTypeDeferredChannelMessage {
		t.Errorf("response type = %d, want %d (deferred)", resp.Type, ResponseTypeDeferredChannelMessage)
	}
}

func TestInteractionHandler_UnknownType(t *testing.T) {
	pub, priv := testKeyPair(t)
	handler := InteractionHandler(pub)

	rr := postInteraction(handler, pub, priv, `{"type":99}`)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestParsePublicKey_Valid(t *testing.T) {
	pub, _ := testKeyPair(t)
	hexKey := hex.EncodeToString(pub)

	parsed, err := ParsePublicKey(hexKey)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if !pub.Equal(parsed) {
		t.Error("parsed key does not match original")
	}
}

func TestParsePublicKey_InvalidHex(t *testing.T) {
	_, err := ParsePublicKey("not-hex!")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestParsePublicKey_WrongLength(t *testing.T) {
	_, err := ParsePublicKey("abcdef")
	if err == nil {
		t.Fatal("expected error for wrong key length")
	}
}
