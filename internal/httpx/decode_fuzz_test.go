package httpx

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Every JSON body in Track goes through DecodeJSON behind BodyLimit: whatever arrives, the answer is
// accepted or a 4xx, never a panic or a 5xx.
func FuzzDecodeJSON(f *testing.F) {
	f.Add([]byte(`{"title":"a","priority":2,"labels":["x"],"meta":{"k":1}}`), "application/json")
	f.Add([]byte(`{"title":1}`), "")
	f.Add([]byte(`{"title":"a"} {"title":"b"}`), "application/json; charset=utf-8")
	f.Add([]byte(`[`), "text/plain")
	f.Add(bytes.Repeat([]byte(`[`), 1<<17), "application/json")
	f.Fuzz(func(t *testing.T, body []byte, contentType string) {
		var dst struct {
			Title    *string        `json:"title"`
			Priority int            `json:"priority"`
			Labels   []string       `json:"labels"`
			Meta     map[string]any `json:"meta"`
		}
		h := BodyLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if DecodeJSON(w, r, &dst) {
				w.WriteHeader(http.StatusOK)
			}
		}))
		req := httptest.NewRequest(http.MethodPost, "/v1/issues", bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK && (w.Code < 400 || w.Code >= 500) {
			t.Fatalf("status %d for %d-byte body", w.Code, len(body))
		}
	})
}
