package lensintegration

import (
	"context"
	"log/slog"
	"net/http"
	"sync"

	"github.com/talyvor/track/internal/authz"
	"github.com/talyvor/track/internal/gatewayauth"
)

// lensBillingRecorder is the slice of workspace.Store RecordBilledTo needs.
type lensBillingRecorder interface {
	RecordLensBilling(ctx context.Context, workspaceID, memberID, lensWorkspaceID string) error
}

// RecordBilledTo notes the Lens workspace the gateway names on an OWNER's authorized workspace request, so
// the syncer can pull that workspace's spend for this one (B17.220). Owners only: a member billed to another
// company's Lens workspace must not pull that company's spend into this workspace. Mount after authz. Each
// pair is written once per process; a failed write is logged and retried on the next request, never blocking it.
func RecordBilledTo(store lensBillingRecorder) func(http.Handler) http.Handler {
	var seen sync.Map
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ws, ok := authz.WorkspaceID(r.Context())
			member, _ := authz.MemberID(r.Context())
			id, idOK := gatewayauth.IdentityFrom(r.Context())
			if ok && idOK && id.LensWorkspace != "" && authz.IsOwner(r.Context()) {
				key := ws + "\x00" + member + "\x00" + id.LensWorkspace
				if _, done := seen.Load(key); !done {
					if err := store.RecordLensBilling(r.Context(), ws, member, id.LensWorkspace); err != nil {
						slog.Warn("lensintegration: cannot record the Lens workspace a workspace is billed to",
							slog.String("workspace_id", ws), slog.String("err", err.Error()))
					} else {
						seen.Store(key, struct{}{})
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
