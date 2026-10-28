package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/talyvor/track/internal/authz"
)

// Any JSON-RPC body a model or client sends gets a JSON-RPC answer — never a panic or a non-JSON reply.
func FuzzHandleRPC(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"create_issue","arguments":{"team_id":"t-1","title":"a"}}}`))
	f.Add([]byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"update_issue","arguments":{"id":"i-1","priority":-9}}}`))
	f.Add([]byte(`{"method":"tools/call","params":{"name":"list_issues","arguments":null}}`))
	f.Add([]byte(`{"method":"tools/call","params":7}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		s := newTestServer(t)
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
		req = req.WithContext(authz.WithMemberships(req.Context(),
			[]authz.Membership{{WorkspaceID: "ws-1", MemberID: "m-test"}}))
		w := httptest.NewRecorder()
		s.HandleRPC(w, req)
		if w.Code >= 500 || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("status %d, body %q", w.Code, w.Body.String())
		}
	})
}
