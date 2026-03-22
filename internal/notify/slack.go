package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

type SlackNotifier struct {
	url        string
	events     map[EventType]bool
	httpClient *http.Client
}

func NewSlackNotifier(url string, filterEvents []string) *SlackNotifier {
	s := &SlackNotifier{
		url: url,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	if len(filterEvents) > 0 {
		s.events = make(map[EventType]bool, len(filterEvents))
		for _, e := range filterEvents {
			s.events[EventType(e)] = true
		}
	}
	return s
}

func (s *SlackNotifier) Notify(event Event) error {
	if s.events != nil && !s.events[event.Type] {
		return nil
	}

	body, err := json.Marshal(struct {
		Text string `json:"text"`
	}{
		Text: formatSlackMessage(event),
	})
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}

		req, err := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "backflow-slack/1.0")

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err
			log.Warn().Err(err).Int("attempt", attempt+1).Str("event", string(event.Type)).Msg("slack request failed")
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Debug().Str("event", string(event.Type)).Str("task_id", event.TaskID).Msg("slack webhook sent")
			return nil
		}
		lastErr = fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
		log.Warn().Int("status", resp.StatusCode).Int("attempt", attempt+1).Msg("slack webhook non-2xx response")
	}

	return fmt.Errorf("slack webhook failed after 3 attempts: %w", lastErr)
}

func (s *SlackNotifier) Name() string { return "slack" }

func formatSlackMessage(event Event) string {
	msg := formatEventMessage(event)
	if event.RepoURL == "" {
		return fmt.Sprintf("*Backflow*\n%s", msg)
	}
	return fmt.Sprintf("*Backflow*\n%s\nRepo: %s", msg, event.RepoURL)
}
