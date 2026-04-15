package messaging

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// TwilioMessenger sends SMS via the Twilio REST API using Basic Auth.
type TwilioMessenger struct {
	accountSID string
	authToken  string
	fromNumber string
	baseURL    string // overridable in tests; defaults to the Twilio API host
	httpClient *http.Client
}

func NewTwilioMessenger(accountSID, authToken, fromNumber string) *TwilioMessenger {
	return &TwilioMessenger{
		accountSID: accountSID,
		authToken:  authToken,
		fromNumber: fromNumber,
		baseURL:    "https://api.twilio.com",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *TwilioMessenger) Send(ctx context.Context, msg OutboundMessage) error {
	apiURL := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", t.baseURL, t.accountSID)

	body := msg.Body
	if body != "" {
		body += "\n"
	}
	body += UnsubscribeFooter

	form := url.Values{}
	form.Set("To", msg.Channel.Address)
	form.Set("From", t.fromNumber)
	form.Set("Body", body)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 2 * time.Second
			select {
			case <-ctx.Done():
				return fmt.Errorf("twilio SMS cancelled during retry: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.SetBasicAuth(t.accountSID, t.authToken)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := t.httpClient.Do(req)
		if err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Msg("twilio request failed")
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			log.Debug().Str("to", msg.Channel.Address).Msg("SMS sent")
			return nil
		}

		// Read error response body for diagnostics
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()

		lastErr = fmt.Errorf("twilio returned status %d: %s", resp.StatusCode, string(respBody))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			log.Warn().Int("status", resp.StatusCode).Str("body", string(respBody)).Msg("twilio client error, not retrying")
			return lastErr
		}
		log.Warn().Int("status", resp.StatusCode).Int("attempt", attempt+1).Msg("twilio server error, retrying")
	}

	return fmt.Errorf("twilio SMS failed after 3 attempts: %w", lastErr)
}
