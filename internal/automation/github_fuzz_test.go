package automation

import (
	"net/http/httptest"
	"testing"

	"github.com/talyvor/track/internal/model"
)

// A correctly signed GitHub delivery of any shape is handled or refused with a 4xx — never a panic or a 5xx.
func FuzzGitHubWebhook(f *testing.F) {
	f.Add("pull_request", []byte(`{"action":"closed","pull_request":{"number":7,"title":"Fixes ENG-42","body":"","merged":true}}`))
	f.Add("pull_request", []byte(`{"action":"opened","pull_request":{"title":"ENG-42 ENG-42","body":null}}`))
	f.Add("push", []byte(`{}`))
	f.Add("pull_request", []byte(`{"pull_request":null}`))
	f.Fuzz(func(t *testing.T, event string, body []byte) {
		fake := &fakeIssueLookup{issuesByIdentifier: map[string]*model.Issue{
			"ENG-42": {ID: "i-1", Identifier: "ENG-42", WorkspaceID: "ws-1"},
		}}
		h := NewGitHubHandler(nil, fake, "topsecret").WithWorkspace("ws-1").WithDeduper(&memDeduper{})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, signedGitHubReq(t, "topsecret", event, body))
		if w.Code >= 500 {
			t.Fatalf("status %d", w.Code)
		}
	})
}
