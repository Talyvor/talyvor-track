package webhooks_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/gatewayauth"
	"github.com/talyvor/track/internal/integrations"
	"github.com/talyvor/track/internal/issue"
	"github.com/talyvor/track/internal/safehttp"
	"github.com/talyvor/track/internal/testutil"
	"github.com/talyvor/track/internal/webhooks"
)

const gwSecret = "test-gateway-transit-secret-0123456789"

func newStore(t *testing.T, d *testutil.DB) *webhooks.Store {
	t.Helper()
	c, err := integrations.NewCipher(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	return webhooks.NewStore(d.Pool, c)
}

func seedMember(t *testing.T, d *testutil.DB, wsID, email, role string) {
	t.Helper()
	if _, err := d.Pool.Exec(context.Background(),
		`INSERT INTO members (workspace_id, name, email, role) VALUES ($1,$2,$3,$4)`, wsID, email, email, role); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}

// chain mounts the issue and webhook handlers behind the real gateway + membership middleware.
func chain(d *testutil.DB, store *webhooks.Store, sink *webhooks.Sink) http.Handler {
	noExempt := func(string) bool { return false }
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Use(gatewayauth.Middleware(gwSecret, noExempt))
		r.Use(authz.Middleware(authz.NewPGResolver(d.Pool), noExempt))
		issue.NewHandler(issue.NewStore(d.Pool)).WithNotifier(sink).Mount(r)
		webhooks.NewHandler(store).Mount(r)
	})
	return r
}

func call(h http.Handler, method, path, email, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gatewayauth.HeaderGatewayAuth, gwSecret)
	req.Header.Set(gatewayauth.HeaderUserEmail, email)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

type received struct {
	headers http.Header
	body    []byte
}

// receiver answers each request with the next status in statuses (the last one repeats).
func receiver(t *testing.T, statuses ...int) (*httptest.Server, func() []received) {
	var mu sync.Mutex
	var got []received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, received{r.Header.Clone(), b})
		status := statuses[min(len(got), len(statuses))-1]
		mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []received { mu.Lock(); defer mu.Unlock(); return append([]received(nil), got...) }
}

// verify checks a delivery the way a customer would, from the Standard Webhooks spec, not via webhooks.Sign.
func verify(t *testing.T, secret string, r received) {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		t.Fatalf("secret is not whsec_<base64>: %q", secret)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(r.headers.Get("webhook-id") + "." + r.headers.Get("webhook-timestamp") + "." + string(r.body)))
	want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if got := r.headers.Get("webhook-signature"); got != want {
		t.Fatalf("webhook-signature = %q, want %q", got, want)
	}
}

func attempts(t *testing.T, d *testutil.DB, wsID string) (n int, codes []int) {
	t.Helper()
	rows, err := d.Pool.Query(context.Background(),
		`SELECT coalesce(status_code, 0) FROM workspace_webhook_deliveries WHERE workspace_id=$1 ORDER BY attempt`, wsID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c int
		_ = rows.Scan(&c)
		codes = append(codes, c)
	}
	return len(codes), codes
}

func TestIssueUpdateReachesURLOnceAndSignatureVerifies(t *testing.T) {
	d := testutil.New(t)
	ws := d.Workspace(t)
	seedMember(t, d, ws.ID, "dev@corp.com", "member")
	iss := d.Issue(t, ws.ID, "")
	srv, got := receiver(t, http.StatusOK)
	store := newStore(t, d)
	hook, secret, err := store.Create(context.Background(), ws.ID, srv.URL, []string{"issue.updated"})
	if err != nil {
		t.Fatal(err)
	}
	sink := webhooks.NewSink(store).WithClient(srv.Client())
	h := chain(d, store, sink)

	if rr := call(h, "PATCH", "/v1/workspaces/"+ws.ID+"/issues/"+iss.ID, "dev@corp.com", `{"title":"Renamed"}`); rr.Code != http.StatusOK {
		t.Fatalf("PATCH issue = %d %s", rr.Code, rr.Body.String())
	}
	// A comment is not subscribed: it must queue nothing for this endpoint.
	if rr := call(h, "POST", "/v1/workspaces/"+ws.ID+"/issues/"+iss.ID+"/comments", "dev@corp.com", `{"body":"hi"}`); rr.Code != http.StatusCreated {
		t.Fatalf("POST comment = %d %s", rr.Code, rr.Body.String())
	}
	for range 3 { // later runs must find nothing left to send
		if _, err := sink.DeliverDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	reqs := got()
	if len(reqs) != 1 {
		t.Fatalf("receiver got %d requests, want exactly 1", len(reqs))
	}
	verify(t, secret, reqs[0])
	var ev struct {
		Type string `json:"type"`
		Data struct {
			IssueID     string         `json:"issue_id"`
			WorkspaceID string         `json:"workspace_id"`
			Changes     map[string]any `json:"changes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(reqs[0].body, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Type != "issue.updated" || ev.Data.IssueID != iss.ID || ev.Data.WorkspaceID != ws.ID || ev.Data.Changes["title"] != "Renamed" {
		t.Fatalf("payload = %s", reqs[0].body)
	}
	if n, codes := attempts(t, d, ws.ID); n != 1 || codes[0] != 200 {
		t.Fatalf("attempt rows = %d %v, want one 200 for webhook %s", n, codes, hook.ID)
	}
}

func TestFailedDeliveryIsRetriedWithTheSameID(t *testing.T) {
	d := testutil.New(t)
	ws := d.Workspace(t)
	iss := d.Issue(t, ws.ID, "")
	srv, got := receiver(t, http.StatusInternalServerError, http.StatusOK)
	store := newStore(t, d)
	_, secret, err := store.Create(context.Background(), ws.ID, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	sink := webhooks.NewSink(store).WithClient(srv.Client())
	ctx := context.Background()

	sink.IssueUpdated(ctx, ws.ID, iss.TeamID, iss.ID, "actor", map[string]any{"priority": 1})
	if _, err := sink.DeliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := sink.DeliverDue(ctx); n != 0 {
		t.Fatalf("retry was sent before its backoff (%d)", n)
	}
	// Bring the scheduled retry forward instead of waiting out the backoff.
	if _, err := d.Pool.Exec(ctx, `UPDATE workspace_webhook_deliveries SET due_at = NOW() WHERE attempted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := sink.DeliverDue(ctx); err != nil {
			t.Fatal(err)
		}
	}

	reqs := got()
	if len(reqs) != 2 {
		t.Fatalf("receiver got %d requests, want 2 (a 500, then its retry)", len(reqs))
	}
	if a, b := reqs[0].headers.Get("webhook-id"), reqs[1].headers.Get("webhook-id"); a == "" || a != b {
		t.Fatalf("retry webhook-id %q, first %q: must be the same message", b, a)
	}
	verify(t, secret, reqs[1])
	if n, codes := attempts(t, d, ws.ID); n != 2 || codes[0] != 500 || codes[1] != 200 {
		t.Fatalf("attempt rows = %d %v, want [500 200]", n, codes)
	}
}

func TestLoopbackURLIsRefused(t *testing.T) {
	d := testutil.New(t)
	ws := d.Workspace(t)
	seedMember(t, d, ws.ID, "owner@corp.com", "owner")
	store := newStore(t, d)
	h := chain(d, store, webhooks.NewSink(store))

	if err := safehttp.Check(context.Background(), "127.0.0.1"); !errors.Is(err, safehttp.ErrBlockedAddress) {
		t.Fatalf("Check(127.0.0.1) = %v, want ErrBlockedAddress", err)
	}
	rr := call(h, "POST", "/v1/workspaces/"+ws.ID+"/webhooks", "owner@corp.com", `{"url":"https://127.0.0.1/hook"}`)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "BLOCKED_ADDRESS") {
		t.Fatalf("create loopback = %d %s, want 400 BLOCKED_ADDRESS", rr.Code, rr.Body.String())
	}

	// A name can resolve to loopback after it was registered: the send itself is refused, and not retried.
	srv, got := receiver(t, http.StatusOK)
	if _, _, err := store.Create(context.Background(), ws.ID, srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	sink := webhooks.NewSink(store) // the real SSRF-guarded client
	sink.IssueDeleted(context.Background(), ws.ID, "team", "issue", "actor")
	if _, err := sink.DeliverDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(got()) != 0 {
		t.Fatal("the guarded client reached a 127.0.0.1 server")
	}
	var total int
	var errText string
	if err := d.Pool.QueryRow(context.Background(),
		`SELECT count(*), max(error) FROM workspace_webhook_deliveries WHERE workspace_id=$1`, ws.ID).Scan(&total, &errText); err != nil {
		t.Fatal(err)
	}
	if total != 1 || !strings.Contains(errText, safehttp.ErrBlockedAddress.Error()) {
		t.Fatalf("rows = %d, error %q: want one refused attempt and no retry", total, errText)
	}
}

func TestOnlyAnOwnerManagesWebhooks(t *testing.T) {
	d := testutil.New(t)
	ws := d.Workspace(t)
	seedMember(t, d, ws.ID, "dev@corp.com", "member")
	seedMember(t, d, ws.ID, "owner@corp.com", "owner")
	store := newStore(t, d)
	h := chain(d, store, webhooks.NewSink(store))
	base := "/v1/workspaces/" + ws.ID + "/webhooks"
	body := `{"url":"https://93.184.215.14/hook","events":["issue.created"]}` // a public literal: no DNS needed

	if rr := call(h, "POST", base, "dev@corp.com", body); rr.Code != http.StatusForbidden {
		t.Fatalf("member create = %d, want 403", rr.Code)
	}
	if rr := call(h, "GET", base, "dev@corp.com", ""); rr.Code != http.StatusForbidden {
		t.Fatalf("member list = %d, want 403", rr.Code)
	}
	if list, _ := store.List(context.Background(), ws.ID); len(list) != 0 {
		t.Fatalf("a refused create stored %d webhooks", len(list))
	}

	rr := call(h, "POST", base, "owner@corp.com", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("owner create = %d %s", rr.Code, rr.Body.String())
	}
	var created struct{ ID, Secret string }
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	if !strings.HasPrefix(created.Secret, "whsec_") {
		t.Fatalf("create returned secret %q", created.Secret)
	}
	if rr := call(h, "GET", base, "owner@corp.com", ""); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), created.Secret) {
		t.Fatalf("list = %d %s: must succeed and never show the secret", rr.Code, rr.Body.String())
	}
	if rr := call(h, "POST", base+"/"+created.ID+"/rotate", "dev@corp.com", ""); rr.Code != http.StatusForbidden {
		t.Fatalf("member rotate = %d, want 403", rr.Code)
	}
	rr = call(h, "POST", base+"/"+created.ID+"/rotate", "owner@corp.com", "")
	var rotated struct{ Secret string }
	_ = json.Unmarshal(rr.Body.Bytes(), &rotated)
	if rr.Code != http.StatusOK || rotated.Secret == created.Secret || !strings.HasPrefix(rotated.Secret, "whsec_") {
		t.Fatalf("rotate = %d %s", rr.Code, rr.Body.String())
	}
	if rr := call(h, "DELETE", base+"/"+created.ID, "dev@corp.com", ""); rr.Code != http.StatusForbidden {
		t.Fatalf("member delete = %d, want 403", rr.Code)
	}
	if rr := call(h, "DELETE", base+"/"+created.ID, "owner@corp.com", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("owner delete = %d", rr.Code)
	}
	if list, _ := store.List(context.Background(), ws.ID); len(list) != 0 {
		t.Fatalf("after delete %d webhooks remain", len(list))
	}
}
