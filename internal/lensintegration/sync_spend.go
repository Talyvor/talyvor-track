package lensintegration

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/talyvor/track/internal/authz"
)

type lensBillingReader interface {
	LensBilling(ctx context.Context) (map[string][]string, error)
}

// SyncSpend is POST /v1/workspaces/{wsID}/lens/sync-spend (B17.242): an owner's BFF hands over the
// caller's own Lens bearer in X-Lens-Token, and Track credits that Lens workspace's per-request spend
// through the syncer's own path. Track's TRACK_LENS_API_KEY is a workspace key that reads only its own
// Lens workspace, so without this a Chat user's issue stayed $0. The token is used for this request only:
// never stored, never logged.
type SyncSpend struct {
	lens    *Client
	updater costUpdater
	billing lensBillingReader
}

func NewSyncSpend(lens *Client, updater costUpdater, billing lensBillingReader) *SyncSpend {
	return &SyncSpend{lens: lens, updater: updater, billing: billing}
}

func (h *SyncSpend) Mount(r chi.Router) {
	r.Post("/workspaces/{wsID}/lens/sync-spend", h.ServeHTTP)
}

func (h *SyncSpend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsID, ok := authz.WorkspaceID(ctx)
	if !ok {
		writeJSON(w, http.StatusForbidden, apiError{Error: "not a member of this workspace", Code: "WORKSPACE_FORBIDDEN"})
		return
	}
	if !authz.IsOwner(ctx) {
		writeJSON(w, http.StatusForbidden, apiError{Error: "owner role required", Code: "OWNER_REQUIRED"})
		return
	}
	if !h.lens.IsConfigured() {
		writeJSON(w, http.StatusServiceUnavailable, apiError{Error: "Lens is not configured", Code: "LENS_NOT_CONFIGURED"})
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("X-Lens-Token"), "Bearer "))
	if token == "" {
		writeJSON(w, http.StatusBadRequest, apiError{Error: "X-Lens-Token is required", Code: "LENS_TOKEN_REQUIRED"})
		return
	}
	caller := New(h.lens.BaseURL(), token)
	id, err := caller.Identity(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: "Lens did not accept the token", Code: "LENS_UNAVAILABLE"})
		return
	}
	// The gateway-named Lens workspace was recorded for an owner by RecordBilledTo, mounted ahead of this.
	billing, err := h.billing.LensBilling(ctx)
	if err != nil {
		slog.Warn("lensintegration: sync-spend cannot read Lens billing", slog.String("workspace_id", wsID), slog.String("err", err.Error()))
		writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal error", Code: "INTERNAL"})
		return
	}
	tws := billing[id.WorkspaceID]
	if id.WorkspaceID == "" || !slices.Contains(tws, wsID) {
		writeJSON(w, http.StatusForbidden, apiError{Error: "the token's Lens workspace is not the one this workspace is billed to", Code: "LENS_WORKSPACE_MISMATCH"})
		return
	}
	n, err := (&Syncer{client: caller, updater: h.updater}).syncLensWorkspace(ctx, id.WorkspaceID, tws)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, apiError{Error: "Lens spend could not be read", Code: "LENS_UNAVAILABLE"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"attributed": n})
}
