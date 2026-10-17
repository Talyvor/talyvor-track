package timetracking_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/testutil"
	"github.com/talyvor/track/internal/timetracking"
)

// B42.7: a time entry is deleted only by the member who logged it or a workspace owner. Bob cannot delete
// Alice's entry (and the row survives); Alice can delete her own; the owner can delete anyone's.
func TestTimeEntryDelete_OwnerOfEntryOrWorkspaceOwner_RealPG(t *testing.T) {
	d := testutil.New(t)
	ctx := context.Background()
	ws := d.Workspace(t)
	iss := d.Issue(t, ws.ID, d.Team(t, ws.ID).ID)
	alice := seedMemberID(t, d, ws.ID, "alice@x.com")
	bob := seedMemberID(t, d, ws.ID, "bob@x.com")
	seedTimeEntry(t, d, iss.ID, ws.ID, alice, "a1")
	seedTimeEntry(t, d, iss.ID, ws.ID, alice, "a2")
	entry := func(desc string) string {
		var id string
		if err := d.Pool.QueryRow(ctx, `SELECT id FROM time_entries WHERE description = $1`, desc).Scan(&id); err != nil {
			return ""
		}
		return id
	}
	r := chi.NewRouter()
	timetracking.NewHandler(timetracking.NewStore(d.Pool)).Mount(r)
	del := func(memberID, role, id string) int {
		req := httptest.NewRequest(http.MethodDelete, "/workspaces/"+ws.ID+"/time-entries/"+id, nil)
		req = req.WithContext(authz.WithAuthorizedRole(req.Context(), ws.ID, memberID, role))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr.Code
	}

	a1 := entry("a1")
	if got := del(bob, authz.RoleMember, a1); got == http.StatusOK {
		t.Fatalf("bob deleted alice's entry: %d", got)
	}
	if entry("a1") == "" {
		t.Fatal("alice's entry is gone after bob's delete")
	}
	if got := del(alice, authz.RoleMember, a1); got != http.StatusOK {
		t.Errorf("alice deleting her own entry = %d, want 200", got)
	}
	if got := del(bob, authz.RoleOwner, entry("a2")); got != http.StatusOK {
		t.Errorf("owner deleting alice's entry = %d, want 200", got)
	}
}
