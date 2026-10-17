package featureboard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/talyvor/track/internal/featureboard"
	"github.com/talyvor/track/internal/testutil"
)

// B42.7: the public board never treats a typed email as proof. A board that requires a verified author
// takes no public post at all, and a vote is removed only with the token issued when it was cast.
func TestPublicBoard_TypedEmailIsNotProof_RealPG(t *testing.T) {
	d := testutil.New(t)
	ctx := context.Background()
	fb := featureboard.NewStore(d.Pool)
	ws := d.Workspace(t)
	open, err := fb.CreateBoard(ctx, featureboard.Board{WorkspaceID: ws.ID, Name: "Open", Slug: "open", Public: true, AllowAnonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	named, err := fb.CreateBoard(ctx, featureboard.Board{WorkspaceID: ws.ID, Name: "Named", Slug: "named", Public: true})
	if err != nil {
		t.Fatal(err)
	}
	post, err := fb.CreatePost(ctx, featureboard.FeaturePost{WorkspaceID: ws.ID, BoardID: open.ID, Title: "Dark mode"})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	featureboard.NewHandler(fb, nil).Mount(r)
	call := func(method, path, body string) (int, map[string]any) {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(method, "/public/boards/"+ws.Slug+"/"+path, strings.NewReader(body)))
		var out map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}
	countVotes := func() int {
		var n int
		if err := d.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM feature_votes WHERE post_id = $1`, post.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// A typed email does not make a post on a verified-only board.
	if code, _ := call(http.MethodPost, named.Slug+"/posts", `{"title":"x","author_email":"ceo@victim.com"}`); code != http.StatusForbidden {
		t.Errorf("post with a typed email to a verified-only board = %d, want 403", code)
	}
	var onNamed int
	if err := d.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM feature_posts WHERE board_id = $1`, named.ID).Scan(&onNamed); err != nil || onNamed != 0 {
		t.Errorf("verified-only board holds %d posts (err %v), want 0", onNamed, err)
	}

	votePath := open.Slug + "/posts/" + post.ID + "/vote"
	code, out := call(http.MethodPost, votePath, `{"email":"alice@x.com"}`)
	token, _ := out["vote_token"].(string)
	if code != http.StatusOK || token == "" {
		t.Fatalf("vote = %d %v, want 200 with a vote_token", code, out)
	}
	// Someone typing alice's email gets no token for her vote.
	if _, again := call(http.MethodPost, votePath, `{"email":"alice@x.com"}`); again["vote_token"] != "" {
		t.Errorf("a repeat vote with alice's email was issued a token: %v", again)
	}
	// Her email alone, or a made-up token, removes nothing.
	for _, body := range []string{`{"email":"alice@x.com"}`, `{"vote_token":"not-the-token"}`} {
		if code, _ := call(http.MethodDelete, votePath, body); code == http.StatusOK {
			t.Errorf("unvote with %s = 200, want refused", body)
		}
	}
	if n := countVotes(); n != 1 {
		t.Fatalf("votes after forged unvotes = %d, want 1", n)
	}
	if code, _ := call(http.MethodDelete, votePath, `{"vote_token":"`+token+`"}`); code != http.StatusOK {
		t.Errorf("unvote with the issued token = %d, want 200", code)
	}
	if n := countVotes(); n != 0 {
		t.Errorf("votes after the real unvote = %d, want 0", n)
	}
}
