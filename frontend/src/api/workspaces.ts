import { apiRequest } from "./client";

export interface WorkspaceSettings {
  id: string;
  name: string;
  logo_url: string;
  email_header_color: string;
  // The caller's own role in this workspace; only an owner may change its settings.
  caller_role: string;
}

export const workspacesApi = {
  get(wsID: string) {
    return apiRequest<WorkspaceSettings>(`/v1/workspaces/${wsID}`);
  },
  update(wsID: string, body: Partial<Pick<WorkspaceSettings, "logo_url" | "email_header_color">>) {
    return apiRequest<WorkspaceSettings>(`/v1/workspaces/${wsID}`, { method: "PATCH", body });
  },
};
