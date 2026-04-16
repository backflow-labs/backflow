package messaging

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/backflow-labs/backflow/internal/config"
	"github.com/backflow-labs/backflow/internal/discord"
	"github.com/backflow-labs/backflow/internal/models"
	"github.com/backflow-labs/backflow/internal/store"
	"github.com/backflow-labs/backflow/internal/taskcreate"
)

// twiMLResponse is a minimal TwiML envelope for replying to inbound SMS.
type twiMLResponse struct {
	XMLName xml.Name      `xml:"Response"`
	Message *twiMLMessage `xml:",omitempty"`
}

type twiMLMessage struct {
	XMLName xml.Name `xml:"Message"`
	Body    string   `xml:",chardata"`
}

var readCommandURLPattern = regexp.MustCompile(`https?://\S+`)

type TaskCreator func(context.Context, *models.CreateTaskRequest) (*models.Task, error)

func writeTwiML(w http.ResponseWriter, msg string) {
	resp := twiMLResponse{}
	if msg != "" {
		resp.Message = &twiMLMessage{Body: msg + "\n" + UnsubscribeFooter}
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp)
}

// validateTwilioSignature checks the X-Twilio-Signature header against the
// HMAC-SHA1 of the request URL + sorted POST parameters, per Twilio's spec.
func validateTwilioSignature(authToken, reqURL, signature string, params url.Values) bool {
	if signature == "" {
		return false
	}

	s := reqURL
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s += k + params.Get(k)
	}

	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(s))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected))
}

// requestURL reconstructs the public-facing URL from the request, respecting
// X-Forwarded-Proto and X-Forwarded-Host headers set by reverse proxies.
// These headers are trusted unconditionally, so this endpoint must sit behind
// a reverse proxy that sets them, or Twilio signature validation must be
// enabled (which binds the URL to the webhook configured in Twilio's console).
func requestURL(r *http.Request) string {
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	u := scheme + "://" + host + r.URL.Path
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	return u
}

// InboundHandler returns an http.HandlerFunc that processes inbound SMS from Twilio.
func parseReadCommand(body string) (string, bool, error) {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return "", false, nil
	}
	if fields[0] != "Read" && fields[0] != "read" {
		return "", false, nil
	}

	tail := strings.TrimSpace(strings.TrimPrefix(body, fields[0]))
	rawURL := readCommandURLPattern.FindString(tail)
	if rawURL == "" {
		return "", true, fmt.Errorf("read commands must include an https URL")
	}

	validated, err := discord.ValidateReadURL(rawURL)
	if err != nil {
		return "", true, err
	}
	return validated, true, nil
}

func InboundHandler(db store.Store, cfg *config.Config, messenger Messenger, createTask TaskCreator) http.HandlerFunc {
	if createTask == nil {
		createTask = func(ctx context.Context, req *models.CreateTaskRequest) (*models.Task, error) {
			return taskcreate.NewTask(ctx, req, db, cfg)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			log.Warn().Err(err).Msg("sms: failed to parse form")
			writeTwiML(w, "Error: could not parse request.")
			return
		}

		// Always validate Twilio request signature — the endpoint must not
		// be mounted unless TwilioAuthToken is configured.
		sig := r.Header.Get("X-Twilio-Signature")
		reqURL := requestURL(r)
		if !validateTwilioSignature(cfg.TwilioAuthToken, reqURL, sig, r.PostForm) {
			log.Warn().Str("url", reqURL).Msg("sms: invalid Twilio signature")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		from := r.FormValue("From")
		body := strings.TrimSpace(r.FormValue("Body"))

		if from == "" || body == "" {
			log.Warn().Str("from", from).Msg("sms: missing From or Body")
			writeTwiML(w, "Error: missing required fields.")
			return
		}

		log.Debug().Str("from", from).Str("body", body).Msg("sms: inbound message received")

		// Look up sender
		sender, err := db.GetAllowedSender(r.Context(), string(ChannelSMS), from)
		if errors.Is(err, store.ErrNotFound) {
			log.Warn().Str("from", from).Msg("sms: rejected message from unknown sender")
			writeTwiML(w, "Sorry, this number is not authorized to create tasks.")
			return
		}
		if err != nil {
			log.Error().Err(err).Str("from", from).Msg("sms: failed to look up sender")
			writeTwiML(w, "Error: internal error.")
			return
		}
		if !sender.Enabled {
			log.Warn().Str("from", from).Msg("sms: rejected message from unknown/disabled sender")
			writeTwiML(w, "Sorry, this number is not authorized to create tasks.")
			return
		}

		replyChannel := fmt.Sprintf("%s:%s", ChannelSMS, from)
		req := &models.CreateTaskRequest{
			Prompt:       body,
			ReplyChannel: replyChannel,
		}

		if readURL, isReadCommand, err := parseReadCommand(body); isReadCommand {
			if err != nil {
				writeTwiML(w, "Error: invalid read command. Use Read https://example.com/article")
				return
			}
			readMode := models.TaskModeRead
			req.Prompt = readURL
			req.TaskMode = &readMode
		}

		task, err := createTask(r.Context(), req)
		if err != nil {
			log.Error().Err(err).Msg("sms: failed to create task")
			writeTwiML(w, "Error: failed to create task.")
			return
		}

		log.Info().Str("task_id", task.ID).Str("from", from).Msg("sms: task created")
		if task.TaskMode == models.TaskModeRead {
			writeTwiML(w, fmt.Sprintf("Reading %s...", task.Prompt))
			return
		}
		writeTwiML(w, fmt.Sprintf("Task created: %s", task.ID))
	}
}
