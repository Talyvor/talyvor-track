import { apiRequest } from "./client";

export interface NotificationPreference {
  kind: string;
  email: boolean;
}

// The caller's own choice; a member who never chose gets assignment mail.
export const notificationsApi = {
  get(wsID: string, kind: string) {
    return apiRequest<NotificationPreference>(
      `/v1/workspaces/${wsID}/notification-preferences/${kind}`,
    );
  },
  set(wsID: string, kind: string, email: boolean) {
    return apiRequest<NotificationPreference>(
      `/v1/workspaces/${wsID}/notification-preferences/${kind}`,
      { method: "PUT", body: { email } },
    );
  },
};
