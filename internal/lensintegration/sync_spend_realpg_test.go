package lensintegration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/issue"
	"github.com/talyvor/track/internal/testutil"
	"github.com/talyvor/track/internal/workspace"
)

// B17.242: an owner's BFF hands Track the caller's own Lens token; Track credits ENG-1 from that Lens
// workspace's spend once, adds nothing on a repeat, and refuses a token whose Lens workspace this
// workspace is not billed to — or any caller who is not an owner.
func TestSyncSpend_CreditsFromTheCallersOwnLensToken(t *testing.T) {
	d := testutil.New(t)
	ctx := context.Background()
	ws := d.Workspace(t)
	eng1 := d.Issue(t, ws.ID, d.Team(t, ws.ID).ID)
	if _, err := d.Pool.Exec(ctx, `UPDATE issues SET identifier='ENG-1' WHERE id=$1`, eng1.ID); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := d.Pool.QueryRow(ctx, `INSERT INTO members (workspace_id, name, email, role) VALUES ($1, 'o', 'o@x.test', 'owner')
		RETURNING id`, ws.ID).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	wsStore := workspace.NewStore(d.Pool)
	if err := wsStore.RecordLensBilling(ctx, ws.ID, owner, "lens-L"); err != nil {
		t.Fatal(err)
	}

	// Like Lens: the bearer decides the workspace; ?workspace_id= cannot widen a workspace token.
	lens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		lensWS := map[string]string{"Bearer tok-L": "lens-L", "Bearer tok-X": "lens-X"}[r.Header.Get("Authorization")]
		switch {
		case lensWS == "":
			w.WriteHeader(http.StatusUnauthorized)
		case r.URL.Path == "/v1/auth/me":
			_, _ = w.Write([]byte(`{"workspace_id":"` + lensWS + `","is_admin":false}`))
		case lensWS == "lens-L":
			_, _ = w.Write([]byte(byRequestBody(`[{"request_id":"req-chat-1","feature":"chat","issue_id":"ENG-1","cost_usd":0.0392,"input_tokens":3330,"output_tokens":118}]`)))
		default:
			_, _ = w.Write([]byte(byRequestBody(`[{"request_id":"req-x-1","feature":"chat","issue_id":"ENG-1","cost_usd":5,"input_tokens":1,"output_tokens":1}]`)))
		}
	}))
	t.Cleanup(lens.Close)
	h := NewSyncSpend(New(lens.URL, "track-workspace-key"), issue.NewStore(d.Pool), wsStore)

	call := func(role, token string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/"+ws.ID+"/lens/sync-spend", nil)
		req.Header.Set("X-Lens-Token", token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req.WithContext(authz.WithAuthorizedRole(ctx, ws.ID, owner, role)))
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	eng1Cost := func() (cost float64, rows int) {
		if err := d.Pool.QueryRow(ctx, `SELECT ai_cost_usd, (SELECT count(*) FROM ai_spend_events WHERE issue_id=$1)
			FROM issues WHERE id=$1`, eng1.ID).Scan(&cost, &rows); err != nil {
			t.Fatal(err)
		}
		return cost, rows
	}

	if code, _ := call("member", "tok-L"); code != http.StatusForbidden {
		t.Fatalf("a member's call = %d, want 403", code)
	}
	if code, _ := call(authz.RoleOwner, "tok-X"); code != http.StatusForbidden {
		t.Fatalf("a token for another Lens workspace = %d, want 403", code)
	}
	if cost, rows := eng1Cost(); cost != 0 || rows != 0 {
		t.Fatalf("refused calls credited ENG-1: ai_cost_usd=%v with %d ledger rows", cost, rows)
	}
	for call1, want := range []string{`{"attributed":1}`, `{"attributed":0}`} {
		if code, body := call(authz.RoleOwner, "tok-L"); code != http.StatusOK || body != want {
			t.Fatalf("call %d = %d %s, want 200 %s", call1+1, code, body, want)
		}
		if cost, rows := eng1Cost(); cost != 0.0392 || rows != 1 {
			t.Fatalf("after call %d: ENG-1 ai_cost_usd=%v with %d ledger rows, want 0.0392 from exactly one row", call1+1, cost, rows)
		}
	}
}
