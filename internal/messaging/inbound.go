package messaging

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/store"
)

// twiMLResponse is a minimal TwiML envelope for replying to inbound SMS.
type twiMLResponse struct {
	XMLName xml.Name     `xml:"Response"`
	Message *twiMLMessage `xml:",omitempty"`
}

type twiMLMessage struct {
	XMLName xml.Name `xml:"Message"`
	Body    string   `xml:",chardata"`
}

func writeTwiML(w http.ResponseWriter, msg string) {
	resp := twiMLResponse{}
	if msg != "" {
		resp.Message = &twiMLMessage{Body: msg}
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp)
}

// InboundHandler returns an http.HandlerFunc that processes inbound SMS from Twilio.
func InboundHandler(db store.Store, cfg *config.Config, messenger Messenger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			log.Warn().Err(err).Msg("sms: failed to parse form")
			writeTwiML(w, "Error: could not parse request.")
			return
		}

		from := r.FormValue("From")
		body := r.FormValue("Body")

		if from == "" || body == "" {
			log.Warn().Str("from", from).Msg("sms: missing From or Body")
			writeTwiML(w, "Error: missing required fields.")
			return
		}

		log.Info().Str("from", from).Str("body", body).Msg("sms: inbound message received")

		// Look up sender
		sender, err := db.GetAllowedSender(r.Context(), string(ChannelSMS), from)
		if err != nil {
			log.Error().Err(err).Str("from", from).Msg("sms: failed to look up sender")
			writeTwiML(w, "Error: internal error.")
			return
		}
		if sender == nil || !sender.Enabled {
			log.Warn().Str("from", from).Msg("sms: rejected message from unknown/disabled sender")
			writeTwiML(w, "Sorry, this number is not authorized to create tasks.")
			return
		}

		// Parse SMS into repo + prompt
		repoURL, prompt, err := ParseTaskFromSMS(body, sender.DefaultRepo)
		if err != nil {
			log.Warn().Err(err).Str("from", from).Msg("sms: failed to parse task")
			writeTwiML(w, fmt.Sprintf("Error: %s", err.Error()))
			return
		}

		now := time.Now().UTC()
		task := &models.Task{
			ID:        "bf_" + ulid.Make().String(),
			Status:    models.TaskStatusPending,
			TaskMode:  models.TaskModeCode,
			Harness:   models.Harness(cfg.DefaultHarness),
			RepoURL:   repoURL,
			Prompt:    prompt,
			Model:     cfg.DefaultModel,
			Effort:    cfg.DefaultEffort,
			CreatePR:  true,
			SelfReview: true,
			SaveAgentOutput: true,
			ReplyChannel: fmt.Sprintf("%s:%s", ChannelSMS, from),
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := db.CreateTask(r.Context(), task); err != nil {
			log.Error().Err(err).Msg("sms: failed to create task")
			writeTwiML(w, "Error: failed to create task.")
			return
		}

		log.Info().Str("task_id", task.ID).Str("from", from).Str("repo", repoURL).Msg("sms: task created")
		writeTwiML(w, fmt.Sprintf("Task created: %s\nRepo: %s", task.ID, repoURL))
	}
}
