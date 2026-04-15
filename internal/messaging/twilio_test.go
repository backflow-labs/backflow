package messaging

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTwilioMessenger_AppendsUnsubscribeFooter(t *testing.T) {
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		gotBody = r.PostFormValue("Body")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"sid":"SMxxx"}`)
	}))
	defer ts.Close()

	m := NewTwilioMessenger("ACxxx", "token", "+15550000000")
	m.baseURL = ts.URL

	err := m.Send(context.Background(), OutboundMessage{
		Channel: Channel{Type: ChannelSMS, Address: "+15551234567"},
		Body:    "Task bf_123 completed.",
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	wantSuffix := "\n" + UnsubscribeFooter
	if !strings.HasSuffix(gotBody, wantSuffix) {
		t.Errorf("posted Body = %q; want suffix %q", gotBody, wantSuffix)
	}
	if !strings.HasPrefix(gotBody, "Task bf_123 completed.") {
		t.Errorf("posted Body did not preserve original text: %q", gotBody)
	}
}
