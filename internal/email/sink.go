package email

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/talyvor/track/internal/model"
)

// KindAssigned is the one email Track sends today; a member gets it unless they turned it off.
const KindAssigned = "issue.assigned"

// MaxAttempts is how many times one message is tried before it is left with its last error.
const MaxAttempts = 5

// lease is how long a claimed row waits before another worker may send it again, if the first never reports.
const lease = 10 * time.Minute

type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// Sink is a notifier for the issue handler: assigning an issue to someone else writes one outbox row for the
// new assignee, and Start's loop sends it. It never fails the request that caused the event.
type Sink struct {
	pool    *pgxpool.Pool
	sender  Sender
	baseURL string
	kick    chan struct{}
}

// NewSink takes the front end's address for the link in each mail; empty leaves the link out.
func NewSink(pool *pgxpool.Pool, sender Sender, baseURL string) *Sink {
	return &Sink{pool: pool, sender: sender, baseURL: baseURL, kick: make(chan struct{}, 1)}
}

func (s *Sink) IssueCreated(ctx context.Context, wsID, _, actorID string, issue model.Issue) {
	if issue.AssigneeID != nil {
		s.assigned(ctx, wsID, issue.ID, *issue.AssigneeID, actorID)
	}
}

func (s *Sink) IssueUpdated(ctx context.Context, wsID, _, issueID, actorID string, changes map[string]any) {
	if id, ok := changes["assignee_id"].(string); ok && id != "" {
		s.assigned(ctx, wsID, issueID, id, actorID)
	}
}

func (s *Sink) IssueDeleted(context.Context, string, string, string, string)          {}
func (s *Sink) CommentCreated(context.Context, string, string, string, model.Comment) {}
func (s *Sink) CommentUpdated(context.Context, string, string, string, model.Comment) {}
func (s *Sink) CommentDeleted(context.Context, string, string, string, string)        {}

func (s *Sink) assigned(ctx context.Context, wsID, issueID, assigneeID, actorID string) {
	if assigneeID == actorID {
		return // nobody needs mail about assigning themselves
	}
	link := ""
	if s.baseURL != "" {
		link = "\n\nOpen Track: " + s.baseURL
	}
	// The request may end the moment this returns; the insert must not be cancelled with it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	// The issue must still name this assignee: the row reads the stored issue, never the request body.
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO notification_outbox (workspace_id, member_id, kind, to_email, subject, body)
		SELECT i.workspace_id, m.id, $4, m.email,
		       i.identifier || ' was assigned to you: ' || i.title,
		       coalesce(a.name, 'Someone') || ' assigned ' || i.identifier || ' to you in ' || w.name || E'.\n\n'
		           || i.identifier || ': ' || i.title || $5
		FROM issues i
		JOIN members m ON m.id = i.assignee_id AND m.workspace_id = i.workspace_id
		JOIN workspaces w ON w.id = i.workspace_id
		LEFT JOIN members a ON a.id = $6 AND a.workspace_id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.id = $2 AND i.assignee_id = $3
		  AND coalesce((SELECT p.email FROM notification_preferences p WHERE p.member_id = m.id AND p.kind = $4), true)`,
		wsID, issueID, assigneeID, KindAssigned, link, actorID)
	if err != nil {
		slog.Error("email: enqueue", "workspace_id", wsID, "issue_id", issueID, "err", err)
		return
	}
	if tag.RowsAffected() > 0 {
		select {
		case s.kick <- struct{}{}:
		default:
		}
	}
}

// Start sends due mail at once (which replays anything left from before a restart), then every interval and
// whenever an event is queued.
func (s *Sink) Start(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		if _, err := s.SendDue(ctx); err != nil && ctx.Err() == nil {
			slog.Error("email: send", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.kick:
		}
	}
}

type outboxRow struct{ id, workspaceID, to, subject, body string }

// SendDue sends every message that is due and returns how many it tried.
func (s *Sink) SendDue(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		WITH due AS (
		    SELECT id FROM notification_outbox
		    WHERE sent_at IS NULL AND attempts < $1 AND due_at <= NOW()
		    ORDER BY due_at LIMIT 50 FOR UPDATE SKIP LOCKED)
		UPDATE notification_outbox SET attempts = attempts + 1, due_at = NOW() + $2 * INTERVAL '1 second'
		WHERE id IN (SELECT id FROM due)
		RETURNING id, workspace_id, to_email, subject, body`, MaxAttempts, int64(lease/time.Second))
	if err != nil {
		return 0, err
	}
	var due []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.id, &r.workspaceID, &r.to, &r.subject, &r.body); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, r := range due {
		if err := s.sender.Send(ctx, r.to, r.subject, r.body); err != nil {
			slog.Warn("email: send failed", "id", r.id, "err", err)
			// Retry after 1, 2, 4, 8 minutes; the lease above covers a crash between claim and here.
			if _, uerr := s.pool.Exec(ctx, `
				UPDATE notification_outbox SET error = $3, due_at = NOW() + power(2, attempts - 1) * INTERVAL '1 minute'
				WHERE workspace_id = $1 AND id = $2`, r.workspaceID, r.id, err.Error()); uerr != nil {
				slog.Error("email: record failure", "id", r.id, "err", uerr)
			}
			continue
		}
		if _, err := s.pool.Exec(ctx, `UPDATE notification_outbox SET sent_at = NOW(), error = '' WHERE workspace_id = $1 AND id = $2`,
			r.workspaceID, r.id); err != nil {
			slog.Error("email: record sent", "id", r.id, "err", err)
		}
	}
	return len(due), nil
}
