package guest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/testutil"
)

// B42.7: inviting, listing and revoking guests is the owner's. A plain member gets 403 on all three; the
// owner, on the same routes and the same guest, gets through — so the 403 is the role gate, not a broken route.
func TestGuestAdmin_OwnerOnly_RealPG(t *testing.T) {
	d := testutil.New(t)
	ws := d.Workspace(t)
	h, gs := guestChain(d)
	inv, err := gs.CreateInvite(context.Background(), ws.ID, nil, "g@x.com", GuestRoleViewer, "owner")
	if err != nil {
		t.Fatalf("invite: %v", err)
	}
	g, err := gs.AcceptInvite(context.Background(), inv.Token, "Guest")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	do := func(role, method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(authz.WithAuthorizedRole(req.Context(), ws.ID, "m1", role))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}
	base := "/workspaces/" + ws.ID + "/guests"
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, base + "/", ""},
		{http.MethodPost, base + "/invite", `{"email":"new@x.com"}`},
		{http.MethodDelete, base + "/" + g.Guest().ID, ""},
	} {
		if got := do(authz.RoleMember, c.method, c.path, c.body); got != http.StatusForbidden {
			t.Errorf("member %s %s = %d, want 403", c.method, c.path, got)
		}
	}
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, base + "/", "", http.StatusOK},
		{http.MethodPost, base + "/invite", `{"email":"new@x.com"}`, http.StatusCreated},
		{http.MethodDelete, base + "/" + g.Guest().ID, "", http.StatusOK},
	} {
		if got := do(authz.RoleOwner, c.method, c.path, c.body); got != c.want {
			t.Errorf("owner %s %s = %d, want %d", c.method, c.path, got, c.want)
		}
	}
}
