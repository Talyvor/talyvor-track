package email_test

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/email"
	"github.com/talyvor/track/internal/gatewayauth"
	"github.com/talyvor/track/internal/issue"
	"github.com/talyvor/track/internal/testutil"
)

const gwSecret = "test-gateway-transit-secret-0123456789"

// smtpStub is a mail server that accepts everything and keeps each message's recipient and data.
func smtpStub(t *testing.T) (email.SMTP, func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var got []string
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				say := func(s string) { _, _ = conn.Write([]byte(s)) }
				say("220 stub\r\n")
				var rcpt string
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						say("250 stub\r\n")
					case strings.HasPrefix(cmd, "RCPT TO:"):
						rcpt = strings.Trim(strings.TrimSpace(line)[8:], "<>")
						say("250 ok\r\n")
					case cmd == "DATA":
						say("354 go\r\n")
						var data strings.Builder
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
							data.WriteString(l)
						}
						mu.Lock()
						got = append(got, rcpt+"\n"+data.String())
						mu.Unlock()
						say("250 queued\r\n")
					case cmd == "QUIT":
						say("221 bye\r\n")
						return
					default:
						say("250 ok\r\n")
					}
				}
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return email.SMTP{Host: "127.0.0.1", Port: port, From: "Track <notifications@talyvor.com>"},
		func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), got...) }
}

func seedMember(t *testing.T, d *testutil.DB, wsID, name, mail string) string {
	t.Helper()
	var id string
	if err := d.Pool.QueryRow(context.Background(),
		`INSERT INTO members (workspace_id, name, email) VALUES ($1,$2,$3) RETURNING id`, wsID, name, mail).Scan(&id); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return id
}

func call(h http.Handler, method, path, mail, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gatewayauth.HeaderGatewayAuth, gwSecret)
	req.Header.Set(gatewayauth.HeaderUserEmail, mail)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestAssignmentSendsOneMailAndACommentSendsNone(t *testing.T) {
	d := testutil.New(t)
	ws := d.Workspace(t)
	seedMember(t, d, ws.ID, "Dana", "dana@corp.com")
	bobID := seedMember(t, d, ws.ID, "Bob", "bob@corp.com")
	iss := d.Issue(t, ws.ID, "")
	stub, got := smtpStub(t)
	sink := email.NewSink(d.Pool, stub, "https://track.example")

	noExempt := func(string) bool { return false }
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		r.Use(gatewayauth.Middleware(gwSecret, noExempt))
		r.Use(authz.Middleware(authz.NewPGResolver(d.Pool), noExempt))
		issue.NewHandler(issue.NewStore(d.Pool)).WithNotifier(sink).Mount(r)
		email.NewHandler(d.Pool).Mount(r)
	})
	base := "/v1/workspaces/" + ws.ID + "/issues/" + iss.ID
	if rr := call(r, "PATCH", base, "dana@corp.com", `{"assignee_id":"`+bobID+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("PATCH assignee = %d %s", rr.Code, rr.Body.String())
	}
	if rr := call(r, "POST", base+"/comments", "dana@corp.com", `{"body":"hi"}`); rr.Code != http.StatusCreated {
		t.Fatalf("POST comment = %d %s", rr.Code, rr.Body.String())
	}
	for range 3 { // later runs must find nothing left to send
		if _, err := sink.SendDue(context.Background()); err != nil {
			t.Fatal(err)
		}
	}

	// Once Bob turns assignment mail off, reassigning him sends nothing more.
	if rr := call(r, "PUT", "/v1/workspaces/"+ws.ID+"/notification-preferences/issue.assigned", "bob@corp.com", `{"email":false}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT preference = %d %s", rr.Code, rr.Body.String())
	}
	if rr := call(r, "PATCH", base, "dana@corp.com", `{"assignee_id":"`+bobID+`"}`); rr.Code != http.StatusOK {
		t.Fatalf("PATCH assignee again = %d %s", rr.Code, rr.Body.String())
	}
	if _, err := sink.SendDue(context.Background()); err != nil {
		t.Fatal(err)
	}

	mails := got()
	if len(mails) != 1 {
		t.Fatalf("stub got %d mails, want exactly 1: %q", len(mails), mails)
	}
	if !strings.HasPrefix(mails[0], "bob@corp.com\n") || !strings.Contains(mails[0], "Dana assigned "+iss.Identifier+" to you") ||
		!strings.Contains(mails[0], "https://track.example") {
		t.Fatalf("mail = %q", mails[0])
	}
	var sent int
	if err := d.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM notification_outbox WHERE workspace_id=$1 AND member_id=$2 AND sent_at IS NOT NULL`, ws.ID, bobID).Scan(&sent); err != nil || sent != 1 {
		t.Fatalf("sent outbox rows = %d (%v), want 1", sent, err)
	}
}

func TestPendingMailIsSentOnceAfterARestart(t *testing.T) {
	d := testutil.New(t)
	ws := d.Workspace(t)
	danaID := seedMember(t, d, ws.ID, "Dana", "dana@corp.com")
	bobID := seedMember(t, d, ws.ID, "Bob", "bob@corp.com")
	iss := d.Issue(t, ws.ID, "")
	ctx := context.Background()
	if _, err := d.Pool.Exec(ctx, `UPDATE issues SET assignee_id=$2 WHERE id=$1`, iss.ID, bobID); err != nil {
		t.Fatal(err)
	}
	// The process that queued the mail stops before its worker runs.
	stub, got := smtpStub(t)
	email.NewSink(d.Pool, stub, "").IssueUpdated(ctx, ws.ID, iss.TeamID, iss.ID, danaID, map[string]any{"assignee_id": bobID})

	restarted, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { email.NewSink(d.Pool, stub, "").Start(restarted, time.Hour); close(done) }()
	waitFor(t, func() bool { return len(got()) > 0 })
	cancel()
	<-done
	if n, err := email.NewSink(d.Pool, stub, "").SendDue(ctx); err != nil || n != 0 {
		t.Fatalf("a second run found %d to send (%v)", n, err)
	}
	if mails := got(); len(mails) != 1 || !strings.HasPrefix(mails[0], "bob@corp.com\n") {
		t.Fatalf("stub got %q, want one mail to bob", mails)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
