package email_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/email"
	"github.com/talyvor/track/internal/gatewayauth"
	"github.com/talyvor/track/internal/testutil"
	"github.com/talyvor/track/internal/workspace"
)

// B39.18: an owner brands the workspace's emails; a branded workspace's mail carries its name, logo and colour,
// an unbranded one Talyvor's default, and a bad colour or a non-https logo is refused.
func TestBrandedWorkspaceMailShowsItsNameLogoAndColour(t *testing.T) {
	d := testutil.New(t)
	ctx := context.Background()
	stub, got := smtpStub(t)
	sink := email.NewSink(d.Pool, stub, "https://track.example")
	noExempt := func(string) bool { return false }
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Use(gatewayauth.Middleware(gwSecret, noExempt))
		r.Use(authz.Middleware(authz.NewPGResolver(d.Pool), noExempt))
		workspace.NewHandler(workspace.NewStore(d.Pool)).Mount(r)
	})

	mailTo := func(wsID, assignee string) string {
		t.Helper()
		by := seedMember(t, d, wsID, "Dana", "dana-"+assignee)
		to := seedMember(t, d, wsID, "Bob", assignee)
		iss := d.Issue(t, wsID, "")
		if _, err := d.Pool.Exec(ctx, `UPDATE issues SET assignee_id=$2 WHERE id=$1`, iss.ID, to); err != nil {
			t.Fatal(err)
		}
		sink.IssueUpdated(ctx, wsID, iss.TeamID, iss.ID, by, map[string]any{"assignee_id": to})
		if _, err := sink.SendDue(ctx); err != nil {
			t.Fatal(err)
		}
		for _, m := range got() {
			if strings.HasPrefix(m, assignee+"\n") {
				return m
			}
		}
		t.Fatalf("no mail to %s in %q", assignee, got())
		return ""
	}

	acme := d.Workspace(t)
	if _, err := d.Pool.Exec(ctx, `UPDATE workspaces SET name='Acme' WHERE id=$1`, acme.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Pool.Exec(ctx, `INSERT INTO members (workspace_id, name, email, role) VALUES ($1,'Olu','olu@acme.com','owner')`, acme.ID); err != nil {
		t.Fatal(err)
	}
	seedMember(t, d, acme.ID, "Mo", "mo@acme.com")
	path := "/v1/workspaces/" + acme.ID
	for _, bad := range []string{`{"email_header_color":"red"}`, `{"email_header_color":"#12345"}`,
		`{"logo_url":"http://acme.example/logo.png"}`, `{"logo_url":"javascript:alert(1)"}`} {
		if rr := call(r, "PATCH", path, "olu@acme.com", bad); rr.Code != http.StatusBadRequest {
			t.Fatalf("PATCH %s = %d %s, want 400", bad, rr.Code, rr.Body.String())
		}
	}
	brand := `{"email_header_color":"#1E40AF","logo_url":"https://acme.example/logo.png"}`
	if rr := call(r, "PATCH", path, "mo@acme.com", brand); rr.Code != http.StatusForbidden {
		t.Fatalf("member PATCH = %d, want 403", rr.Code)
	}
	if rr := call(r, "PATCH", path, "olu@acme.com", brand); rr.Code != http.StatusOK ||
		!strings.Contains(rr.Body.String(), `"email_header_color":"#1E40AF"`) {
		t.Fatalf("owner PATCH = %d %s", rr.Code, rr.Body.String())
	}

	branded := mailTo(acme.ID, "bob@acme.com")
	for _, want := range []string{`From: "Acme via Talyvor" <notifications@talyvor.com>`, "Content-Type: text/html",
		`<img src="https://acme.example/logo.png" alt="Acme"`, "background:#1E40AF", "assigned"} {
		if !strings.Contains(branded, want) {
			t.Fatalf("branded mail lacks %q:\n%s", want, branded)
		}
	}

	plain := mailTo(d.Workspace(t).ID, "bob@plain.com")
	for _, want := range []string{"From: Track <notifications@talyvor.com>", "background:#060A12",
		`<img src="https://track.example/email-logo.png" alt="Talyvor"`} {
		if !strings.Contains(plain, want) {
			t.Fatalf("unbranded mail lacks %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "via Talyvor") {
		t.Fatalf("unbranded mail carries a workspace From name:\n%s", plain)
	}
}
