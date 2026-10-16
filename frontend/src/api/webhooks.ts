import { apiRequest } from "./client";

export const WEBHOOK_EVENTS = [
  "issue.created",
  "issue.updated",
  "issue.deleted",
  "comment.created",
  "comment.updated",
  "comment.deleted",
] as const;

export interface Webhook {
  id: string;
  url: string;
  events: string[];
  created_at: string;
}

// Owner-only. The secret is returned by create and rotate, never by list.
export const webhooksApi = {
  list(wsID: string) {
    return apiRequest<Webhook[]>(`/v1/workspaces/${wsID}/webhooks`);
  },
  create(wsID: string, body: { url: string; events: string[] }) {
    return apiRequest<Webhook & { secret: string }>(`/v1/workspaces/${wsID}/webhooks`, {
      method: "POST",
      body,
    });
  },
  rotate(wsID: string, id: string) {
    return apiRequest<{ id: string; secret: string }>(
      `/v1/workspaces/${wsID}/webhooks/${id}/rotate`,
      { method: "POST" },
    );
  },
  remove(wsID: string, id: string) {
    return apiRequest<void>(`/v1/workspaces/${wsID}/webhooks/${id}`, { method: "DELETE" });
  },
};
