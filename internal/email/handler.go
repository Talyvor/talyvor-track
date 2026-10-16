package email

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/httpx"
)

// Handler is GET and PUT /v1/workspaces/{wsID}/notification-preferences/{kind}: the caller reads, or turns on
// or off, one kind of email for themselves, or (kind digest) one daily digest instead of a mail per event.
type Handler struct{ pool *pgxpool.Pool }

// kindDefault is each kind's value when the member never chose, as the sink reads it.
var kindDefault = map[string]bool{KindAssigned: true, KindDigest: false}

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) Mount(r chi.Router) {
	r.Get("/workspaces/{wsID}/notification-preferences/{kind}", h.get)
	r.Put("/workspaces/{wsID}/notification-preferences/{kind}", h.put)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	wsID, ok := authz.WorkspaceID(r.Context())
	memberID, ok2 := authz.MemberID(r.Context())
	if !ok || !ok2 {
		writeErr(w, http.StatusForbidden, "FORBIDDEN", "workspace not authorized")
		return
	}
	kind := chi.URLParam(r, "kind")
	def, known := kindDefault[kind]
	if !known {
		writeErr(w, http.StatusNotFound, "UNKNOWN_KIND", "unknown notification kind")
		return
	}
	var on bool
	err := h.pool.QueryRow(r.Context(), `
		SELECT coalesce((SELECT p.email FROM notification_preferences p WHERE p.member_id = m.id AND p.kind = $3), $4)
		FROM members m WHERE m.workspace_id = $1 AND m.id = $2`, wsID, memberID, kind, def).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusForbidden, "FORBIDDEN", "workspace not authorized")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "READ_FAILED", "could not read the preference")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "email": on})
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	wsID, ok := authz.WorkspaceID(r.Context())
	memberID, ok2 := authz.MemberID(r.Context())
	if !ok || !ok2 {
		writeErr(w, http.StatusForbidden, "FORBIDDEN", "workspace not authorized")
		return
	}
	kind := chi.URLParam(r, "kind")
	if _, known := kindDefault[kind]; !known {
		writeErr(w, http.StatusNotFound, "UNKNOWN_KIND", "unknown notification kind")
		return
	}
	var in struct {
		Email *bool `json:"email"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.Email == nil {
		writeErr(w, http.StatusBadRequest, "EMAIL_REQUIRED", "email (true or false) is required")
		return
	}
	// The member row is read inside the workspace, so the preference can only belong to a member of it.
	tag, err := h.pool.Exec(r.Context(), `
		INSERT INTO notification_preferences (workspace_id, member_id, kind, email)
		SELECT workspace_id, id, $3, $4 FROM members WHERE workspace_id = $1 AND id = $2
		ON CONFLICT (member_id, kind) DO UPDATE SET email = EXCLUDED.email, updated_at = NOW()`,
		wsID, memberID, kind, *in.Email)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "SAVE_FAILED", "could not save the preference")
		return
	}
	if tag.RowsAffected() == 0 {
		writeErr(w, http.StatusForbidden, "FORBIDDEN", "workspace not authorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "email": *in.Email})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}
