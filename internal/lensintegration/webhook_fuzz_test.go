package lensintegration

import (
	"net/http/httptest"
	"testing"
)

// A correctly signed body of any shape is handled or refused with a 4xx — never a panic or a 5xx.
func FuzzSpendAlertWebhook(f *testing.F) {
	f.Add([]byte(`{"type":"spend_alert","workspace_id":"ws-1","feature":"ENG-1","cost_usd":1.5,"threshold":1}`))
	f.Add([]byte(`{"type":"spend_alert","cost_usd":-1e308,"emitted_at":"not-a-time","event_id":""}`))
	f.Add([]byte(`{"type":7}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, body []byte) {
		wh := NewWebhookHandler("topsecret", &recordingIssueLookup{}, &recordingNotifications{}, &recordingNotifier{})
		w := httptest.NewRecorder()
		wh.ServeHTTP(w, signedRequest(t, "topsecret", body))
		if w.Code >= 500 {
			t.Fatalf("status %d", w.Code)
		}
	})
}
