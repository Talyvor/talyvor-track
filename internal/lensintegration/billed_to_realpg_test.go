package lensintegration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/gatewayauth"
	"github.com/talyvor/track/internal/issue"
	"github.com/talyvor/track/internal/testutil"
	"github.com/talyvor/track/internal/workspace"
)

// B17.220: a Chat answer naming ENG-1 is charged under the caller's LENS workspace, which Track never
// minted. A request that names that Lens workspace records it for the Track workspace, and the syncer
// then pulls the Lens workspace's spend and credits ENG-1 once; a re-pull adds nothing.
func TestSyncer_CreditsSpendFromTheLensWorkspaceTheWorkspaceIsBilledTo(t *testing.T) {
	d := testutil.New(t)
	ctx := context.Background()
	ws := d.Workspace(t)
	eng1 := d.Issue(t, ws.ID, d.Team(t, ws.ID).ID)
	if _, err := d.Pool.Exec(ctx, `UPDATE issues SET identifier='ENG-1' WHERE id=$1`, eng1.ID); err != nil {
		t.Fatal(err)
	}
	wsStore := workspace.NewStore(d.Pool)

	// A gateway request on the workspace, billed to Lens workspace lens-L.
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+ws.ID+"/issues", nil)
	rctx := gatewayauth.WithIdentity(authz.WithAuthorized(ctx, ws.ID, "m-1"),
		gatewayauth.Identity{Email: "a@x.test", LensWorkspace: "lens-L"})
	RecordBilledTo(wsStore)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(httptest.NewRecorder(), req.WithContext(rctx))

	lens := httptest.NewServer(asAdminLens(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("workspace_id") == "lens-L" {
			_, _ = w.Write([]byte(byRequestBody(`[{"request_id":"req-chat-1","feature":"chat","issue_id":"ENG-1","cost_usd":0.0392,"input_tokens":3330,"output_tokens":118}]`)))
			return
		}
		_, _ = w.Write([]byte(byRequestBody(`[]`)))
	}))
	t.Cleanup(lens.Close)
	s := NewSyncer(New(lens.URL, "k"), issue.NewStore(d.Pool), wsStore)

	for pull := 1; pull <= 2; pull++ {
		s.runOnce(ctx)
		var cost float64
		var rows int
		if err := d.Pool.QueryRow(ctx, `SELECT ai_cost_usd, (SELECT count(*) FROM ai_spend_events WHERE issue_id=$1)
			FROM issues WHERE id=$1`, eng1.ID).Scan(&cost, &rows); err != nil {
			t.Fatal(err)
		}
		if cost != 0.0392 || rows != 1 {
			t.Fatalf("after pull %d: ENG-1 ai_cost_usd=%v with %d ledger rows, want 0.0392 from exactly one row", pull, cost, rows)
		}
	}
}
