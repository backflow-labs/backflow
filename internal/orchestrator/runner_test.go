package orchestrator

import (
	"encoding/json"
	"testing"
)

func TestAgentStatus_ParsesReadingFields(t *testing.T) {
	raw := []byte(`{
		"complete": true,
		"url": "https://example.com/post",
		"title": "Post title",
		"tldr": "Short summary",
		"tags": ["ai", "systems"],
		"keywords": ["embeddings", "retrieval"],
		"people": ["Ada"],
		"orgs": ["Example Corp"],
		"novelty_verdict": "new",
		"connections": [
			{"reading_id": "bf_other", "reason": "same topic"}
		],
		"summary_markdown": "# Long summary"
	}`)

	var got AgentStatus
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.URL != "https://example.com/post" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.Title != "Post title" {
		t.Errorf("Title = %q", got.Title)
	}
	if got.TLDR != "Short summary" {
		t.Errorf("TLDR = %q", got.TLDR)
	}
	if got.NoveltyVerdict != "new" {
		t.Errorf("NoveltyVerdict = %q", got.NoveltyVerdict)
	}
	if got.SummaryMarkdown != "# Long summary" {
		t.Errorf("SummaryMarkdown = %q", got.SummaryMarkdown)
	}
	if want := []string{"ai", "systems"}; !equalStrings(got.Tags, want) {
		t.Errorf("Tags = %v, want %v", got.Tags, want)
	}
	if want := []string{"embeddings", "retrieval"}; !equalStrings(got.Keywords, want) {
		t.Errorf("Keywords = %v, want %v", got.Keywords, want)
	}
	if want := []string{"Ada"}; !equalStrings(got.People, want) {
		t.Errorf("People = %v, want %v", got.People, want)
	}
	if want := []string{"Example Corp"}; !equalStrings(got.Orgs, want) {
		t.Errorf("Orgs = %v, want %v", got.Orgs, want)
	}
	if len(got.Connections) != 1 || got.Connections[0].ReadingID != "bf_other" || got.Connections[0].Reason != "same topic" {
		t.Errorf("Connections = %+v", got.Connections)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
