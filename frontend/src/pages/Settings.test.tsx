import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useWorkspaceStore } from "~/stores/workspace";
import { SettingsPage } from "./Settings";

// Settings against a fake Track API: only fetch is replaced, so a PATCH here is the request the browser sends.
function fakeTrack(role: string, patchStatus = 200) {
  const patches: Record<string, unknown>[] = [];
  const ws = { id: "ws-1", name: "Acme", logo_url: "", email_header_color: "", caller_role: role };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (body: unknown, status = 200) =>
        new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
      if (url.endsWith("/workspaces/ws-1")) {
        if (init?.method !== "PATCH") return json(ws);
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        patches.push(body);
        if (patchStatus !== 200) return json({ error: "logo_url must be an https URL", code: "INVALID_BRANDING" }, patchStatus);
        return json({ ...ws, ...body });
      }
      return json([]);
    }),
  );
  return patches;
}

function renderSettings() {
  useWorkspaceStore.setState({ workspaceId: "ws-1" });
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <SettingsPage />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("Settings email branding", () => {
  it("an owner saves a colour and logo and the PATCH carries them", async () => {
    const patches = fakeTrack("owner");
    renderSettings();
    const color = await screen.findByLabelText("Header colour");
    fireEvent.change(color, { target: { value: "#1E6BFF" } });
    fireEvent.change(screen.getByLabelText("Logo URL"), { target: { value: "https://acme.example/logo.png" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(patches).toHaveLength(1));
    expect(patches[0]).toEqual({ email_header_color: "#1E6BFF", logo_url: "https://acme.example/logo.png" });
    const preview = screen.getByLabelText("Email header preview");
    expect(preview.querySelector("img")?.getAttribute("src")).toBe("https://acme.example/logo.png");
  });

  it("shows the server's message when it refuses a value", async () => {
    fakeTrack("owner", 400);
    renderSettings();
    fireEvent.change(await screen.findByLabelText("Logo URL"), { target: { value: "http://acme.example/logo.png" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect((await screen.findByRole("alert")).textContent).toBe("logo_url must be an https URL");
  });

  it("a member sees no email branding section", async () => {
    fakeTrack("member");
    renderSettings();
    await screen.findByText("Webhooks");
    await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([u]) => String(u).endsWith("/workspaces/ws-1"))).toBe(true));
    expect(screen.queryByText("Email branding")).toBeNull();
    expect(screen.queryByLabelText("Header colour")).toBeNull();
  });
});
