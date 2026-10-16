import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, RefreshCw, X } from "lucide-react";
import { useWorkspaceStore } from "~/stores/workspace";
import { teamsApi } from "~/api/teams";
import { guestsApi } from "~/api/guests";
import { WEBHOOK_EVENTS, webhooksApi, type Webhook } from "~/api/webhooks";
import { notificationsApi } from "~/api/notifications";
import { workspacesApi } from "~/api/workspaces";
import { Input } from "~/components/ui/Input";
import { Button } from "~/components/ui/Button";
import { useUIStore } from "~/stores/ui";
import type { GuestRecord, GuestRole, InviteCreateResponse } from "~/api/types";

export function SettingsPage() {
  const { workspaceId, memberId, teamId, setWorkspaceId, setMemberId, setTeamId } =
    useWorkspaceStore();
  const teams = useQuery({
    queryKey: ["teams", workspaceId],
    queryFn: () => teamsApi.list(workspaceId),
    enabled: !!workspaceId,
  });
  return (
    <div className="max-w-2xl space-y-6 p-6">
      <section>
        <h2 className="mb-2 text-sm font-semibold">Workspace</h2>
        <p className="mb-2 text-xs text-muted">
          Pre-auth-shim. Phase 9 replaces this with a real session.
        </p>
        <Field label="Workspace ID">
          <Input
            value={workspaceId}
            onChange={(e) => setWorkspaceId(e.target.value)}
            placeholder="default"
          />
        </Field>
        <Field label="Your Member ID">
          <Input
            value={memberId}
            onChange={(e) => setMemberId(e.target.value)}
            placeholder="optional"
          />
        </Field>
      </section>
      <section>
        <h2 className="mb-2 text-sm font-semibold">Active team</h2>
        {teams.isLoading ? (
          <p className="text-xs text-muted">Loading teams…</p>
        ) : (teams.data ?? []).length === 0 ? (
          <p className="text-xs text-muted">No teams yet — create one via the API.</p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {teams.data!.map((t) => (
              <Button
                key={t.id}
                variant={t.id === teamId ? "primary" : "secondary"}
                size="sm"
                onClick={() => setTeamId(t.id)}
              >
                {t.identifier} · {t.name}
              </Button>
            ))}
          </div>
        )}
      </section>
      <section>
        <h2 className="mb-2 text-sm font-semibold">API key</h2>
        <p className="mb-2 text-xs text-muted">
          Stored in <code>localStorage.track_api_key</code>. We never log or transmit it elsewhere.
        </p>
        <Input
          defaultValue={localStorage.getItem("track_api_key") ?? ""}
          placeholder="track_…"
          onBlur={(e) => localStorage.setItem("track_api_key", e.target.value)}
        />
      </section>
      <EmailSection />
      <EmailBrandingSection />
      <GuestsSection />
      <WebhooksSection />
    </div>
  );
}

// EmailSection is the caller's own mail choice; assignment mail is on, and the daily digest off, until they
// choose otherwise.
function EmailSection() {
  const { workspaceId } = useWorkspaceStore();
  const pref = useMailPreference(workspaceId, "issue.assigned");
  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold">Email</h2>
      {pref.isError ? (
        <p className="text-xs text-muted">Join this workspace as a member to choose your email.</p>
      ) : (
        <div className="space-y-2">
          <MailToggle kind="issue.assigned" fallback>
            Email me when an issue is assigned to me
          </MailToggle>
          <MailToggle kind="digest" fallback={false}>
            Send me one daily digest at 09:00 UTC instead of a mail per event
          </MailToggle>
        </div>
      )}
    </section>
  );
}

function useMailPreference(workspaceId: string, kind: string) {
  return useQuery({
    queryKey: ["notification-preferences", workspaceId, kind],
    queryFn: () => notificationsApi.get(workspaceId, kind),
    enabled: !!workspaceId,
    retry: false,
  });
}

function MailToggle({ kind, fallback, children }: { kind: string; fallback: boolean; children: ReactNode }) {
  const { workspaceId } = useWorkspaceStore();
  const qc = useQueryClient();
  const toast = useUIStore((s) => s.toast);
  const pref = useMailPreference(workspaceId, kind);
  const save = useMutation({
    mutationFn: (email: boolean) => notificationsApi.set(workspaceId, kind, email),
    onSuccess: (res) => qc.setQueryData(["notification-preferences", workspaceId, kind], res),
    onError: (err: Error) => toast(err.message, "error"),
  });
  return (
    <label className="flex items-center gap-2 text-xs">
      <input
        type="checkbox"
        className="accent-accent"
        checked={pref.data?.email ?? fallback}
        disabled={pref.isLoading || save.isPending}
        onChange={(e) => save.mutate(e.target.checked)}
      />
      {children}
    </label>
  );
}

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;
const OBSIDIAN = "#060A12";

// EmailBrandingSection lets a workspace owner set the colour and logo at the top of every Track mail; the
// server refuses a value that is not #rrggbb or an https URL, and members never see the section.
function EmailBrandingSection() {
  const { workspaceId } = useWorkspaceStore();
  const ws = useQuery({
    queryKey: ["workspace", workspaceId],
    queryFn: () => workspacesApi.get(workspaceId),
    enabled: !!workspaceId,
    retry: false,
  });
  if (ws.data?.caller_role !== "owner") return null;
  return <EmailBrandingForm key={ws.data.id} initial={ws.data} />;
}

function EmailBrandingForm({ initial }: { initial: { name: string; email_header_color: string; logo_url: string } }) {
  const { workspaceId } = useWorkspaceStore();
  const qc = useQueryClient();
  const toast = useUIStore((s) => s.toast);
  const [color, setColor] = useState(initial.email_header_color);
  const [logo, setLogo] = useState(initial.logo_url);
  const save = useMutation({
    mutationFn: () =>
      workspacesApi.update(workspaceId, { email_header_color: color.trim(), logo_url: logo.trim() }),
    onSuccess: (res) => {
      qc.setQueryData(["workspace", workspaceId], res);
      toast("Email branding saved", "success");
    },
  });
  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold">Email branding</h2>
      <p className="mb-2 text-xs text-muted">
        The colour and logo at the top of every email this workspace sends. Leave both empty for Talyvor's
        default. Workspace owners only.
      </p>
      <div className="mb-3 space-y-2 rounded-md border border-border bg-bg/40 p-2">
        <div className="flex items-center gap-2">
          <input
            type="color"
            aria-label="Pick header colour"
            value={HEX_COLOR.test(color) ? color : OBSIDIAN}
            onChange={(e) => setColor(e.target.value)}
            className="h-9 w-9 shrink-0 cursor-pointer rounded border border-border bg-bg"
          />
          <Input
            aria-label="Header colour"
            placeholder={OBSIDIAN}
            value={color}
            onChange={(e) => setColor(e.target.value)}
            className="font-mono"
          />
        </div>
        <Input
          aria-label="Logo URL"
          placeholder="https://example.com/logo.png"
          value={logo}
          onChange={(e) => setLogo(e.target.value)}
        />
        {/* The logo previews once saved: the server has checked it is https by then. */}
        <MailHeaderPreview name={initial.name} color={color.trim()} logo={initial.logo_url} />
        {save.isError ? (
          <p role="alert" className="text-xs text-priority-urgent">
            {save.error.message}
          </p>
        ) : null}
        <Button size="sm" onClick={() => save.mutate()} disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save"}
        </Button>
      </div>
    </section>
  );
}

// MailHeaderPreview draws the header as internal/email/layout.go renders it: nothing set is Talyvor's
// Obsidian and logo; a colour with no logo shows the workspace name in ink that reads on it.
function MailHeaderPreview({ name, color, logo }: { name: string; color: string; logo: string }) {
  const unbranded = !color && !logo;
  const bg = HEX_COLOR.test(color) ? color : OBSIDIAN;
  const src = unbranded ? "/email-logo.png" : logo.startsWith("https://") ? logo : "";
  return (
    <div aria-label="Email header preview" className="overflow-hidden rounded-md border border-border">
      <div style={{ background: bg }} className="px-6 py-5">
        {src ? (
          <img src={src} alt={unbranded ? "Talyvor" : name} className="block h-8 max-w-full" />
        ) : (
          <span className="text-xl font-semibold" style={{ color: ink(bg) }}>
            {name}
          </span>
        )}
      </div>
      <div className="bg-white px-6 py-3 text-xs text-[#060A12]">TRK-12 was assigned to you.</div>
    </div>
  );
}

function ink(bg: string) {
  const v = parseInt(bg.slice(1), 16);
  return 0.299 * ((v >> 16) & 0xff) + 0.587 * ((v >> 8) & 0xff) + 0.114 * (v & 0xff) > 150 ? OBSIDIAN : "#FFFFFF";
}

// GuestsSection embeds the guest-management surface in Settings. We
// keep it inline (not a separate page) because it's an admin-style
// list with one expanded "Invite" form — splitting would add nav
// friction without adding value.
function GuestsSection() {
  const { workspaceId } = useWorkspaceStore();
  const qc = useQueryClient();
  const toast = useUIStore((s) => s.toast);

  const guests = useQuery({
    queryKey: ["guests", workspaceId],
    queryFn: () => guestsApi.list(workspaceId),
    enabled: !!workspaceId,
  });
  const revoke = useMutation({
    mutationFn: (id: string) => guestsApi.revoke(workspaceId, id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["guests"] }),
    onError: (err: Error) => toast(err.message, "error"),
  });
  const invite = useMutation({
    mutationFn: (vars: { email: string; role: GuestRole }) =>
      guestsApi.invite(workspaceId, vars),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["guests"] }),
    onError: (err: Error) => toast(err.message, "error"),
  });

  const [email, setEmail] = useState("");
  const [role, setRole] = useState<GuestRole>("viewer");
  const [lastInvite, setLastInvite] = useState<InviteCreateResponse | null>(null);

  const submit = () => {
    if (!email.trim()) {
      toast("Email required", "warn");
      return;
    }
    invite.mutate(
      { email: email.trim(), role },
      {
        onSuccess: (res) => {
          setLastInvite(res);
          setEmail("");
        },
      },
    );
  };

  const copy = (text: string) => {
    void navigator.clipboard.writeText(text);
    toast("Copied to clipboard", "success");
  };

  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold">Guests</h2>
      <p className="mb-2 text-xs text-muted">
        Invite clients, contractors, or stakeholders. Free on every plan — no per-seat charges.
      </p>

      <div className="mb-3 flex items-center gap-2 rounded-md border border-border bg-bg/40 p-2">
        <Input
          placeholder="guest@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <select
          value={role}
          onChange={(e) => setRole(e.target.value as GuestRole)}
          className="h-9 rounded border border-border bg-bg px-2 text-xs"
        >
          <option value="viewer">Viewer</option>
          <option value="commenter">Commenter</option>
          <option value="editor">Editor</option>
        </select>
        <Button size="sm" onClick={submit} disabled={invite.isPending}>
          {invite.isPending ? "Inviting…" : "Invite"}
        </Button>
      </div>

      {lastInvite ? (
        <div className="mb-3 rounded-md border border-accent/40 bg-accent/5 p-3 text-xs">
          <div className="mb-1 font-medium">Invite created</div>
          <div className="mb-2 text-muted">
            Send this link — it expires{" "}
            {new Date(lastInvite.expires_at).toLocaleDateString()}.
          </div>
          <div className="flex items-center gap-2">
            <code className="flex-1 truncate rounded bg-bg px-2 py-1 font-mono text-[10px]">
              {lastInvite.invite_url}
            </code>
            <button
              onClick={() => copy(lastInvite.invite_url)}
              className="text-muted hover:text-text"
              title="Copy"
            >
              <Copy size={12} />
            </button>
          </div>
        </div>
      ) : null}

      <div className="space-y-1">
        {guests.isLoading ? (
          <div className="text-xs text-muted">Loading…</div>
        ) : (guests.data ?? []).length === 0 ? (
          <div className="text-xs text-muted">No guests yet.</div>
        ) : (
          (guests.data ?? []).map((g: GuestRecord) => (
            <div
              key={g.id}
              className="flex items-center gap-2 rounded-md border border-border bg-bg px-2 py-1.5 text-xs"
            >
              <div className="flex-1 truncate">
                <span className="font-medium">{g.name || g.email}</span>
                <span className="ml-1 text-muted">{g.email}</span>
              </div>
              <span className="text-[10px] uppercase tracking-wider text-muted">{g.role}</span>
              {!g.active ? (
                <span className="text-[10px] text-priority-urgent">revoked</span>
              ) : null}
              {g.active ? (
                <button
                  onClick={() => revoke.mutate(g.id)}
                  className="text-muted hover:text-priority-urgent"
                  title="Revoke access"
                >
                  <X size={12} />
                </button>
              ) : null}
            </div>
          ))
        )}
      </div>
    </section>
  );
}

// WebhooksSection lets a workspace owner send issue and comment events to their own URL. Each
// request is signed with the endpoint's secret, which is shown once, on create or rotate.
function WebhooksSection() {
  const { workspaceId } = useWorkspaceStore();
  const qc = useQueryClient();
  const toast = useUIStore((s) => s.toast);
  const [url, setUrl] = useState("");
  const [events, setEvents] = useState<string[]>([]);
  const [shownSecret, setShownSecret] = useState<{ url: string; secret: string } | null>(null);

  const hooks = useQuery({
    queryKey: ["webhooks", workspaceId],
    queryFn: () => webhooksApi.list(workspaceId),
    enabled: !!workspaceId,
    retry: false,
  });
  const done = () => qc.invalidateQueries({ queryKey: ["webhooks"] });
  const onError = (err: Error) => toast(err.message, "error");
  const create = useMutation({
    mutationFn: () => webhooksApi.create(workspaceId, { url: url.trim(), events }),
    onSuccess: (res) => {
      setShownSecret({ url: res.url, secret: res.secret });
      setUrl("");
      setEvents([]);
      void done();
    },
    onError,
  });
  const rotate = useMutation({
    mutationFn: (h: Webhook) => webhooksApi.rotate(workspaceId, h.id),
    onSuccess: (res, h) => setShownSecret({ url: h.url, secret: res.secret }),
    onError,
  });
  const remove = useMutation({
    mutationFn: (id: string) => webhooksApi.remove(workspaceId, id),
    onSuccess: done,
    onError,
  });

  const toggle = (e: string) =>
    setEvents((cur) => (cur.includes(e) ? cur.filter((x) => x !== e) : [...cur, e]));

  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold">Webhooks</h2>
      <p className="mb-2 text-xs text-muted">
        Send issue and comment events to your own HTTPS endpoint. Every request carries the
        webhook-id, webhook-timestamp and webhook-signature headers; verify them with the
        endpoint's secret. A failed delivery is retried six times over about 18 hours. Workspace owners only.
      </p>

      {hooks.isError ? (
        <div className="text-xs text-muted">Only a workspace owner can manage webhooks.</div>
      ) : (
        <>
          <div className="mb-3 space-y-2 rounded-md border border-border bg-bg/40 p-2">
            <div className="flex items-center gap-2">
              <Input
                placeholder="https://example.com/track-events"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                aria-label="Webhook URL"
              />
              <Button
                size="sm"
                onClick={() => (url.trim() ? create.mutate() : toast("URL required", "warn"))}
                disabled={create.isPending}
              >
                {create.isPending ? "Adding…" : "Add"}
              </Button>
            </div>
            <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
              {WEBHOOK_EVENTS.map((e) => (
                <label key={e} className="flex items-center gap-1 font-mono text-[10px]">
                  <input type="checkbox" checked={events.includes(e)} onChange={() => toggle(e)} />
                  {e}
                </label>
              ))}
              <span className="text-[10px] text-muted">None ticked sends every event.</span>
            </div>
          </div>

          {shownSecret ? (
            <div className="mb-3 rounded-md border border-accent/40 bg-accent/5 p-3 text-xs">
              <div className="mb-1 font-medium">Signing secret for {shownSecret.url}</div>
              <div className="mb-2 text-muted">Copy it now — it is not shown again.</div>
              <div className="flex items-center gap-2">
                <code className="flex-1 truncate rounded bg-bg px-2 py-1 font-mono text-[10px]">
                  {shownSecret.secret}
                </code>
                <button
                  onClick={() => {
                    void navigator.clipboard.writeText(shownSecret.secret);
                    toast("Copied to clipboard", "success");
                  }}
                  className="text-muted hover:text-text"
                  title="Copy"
                >
                  <Copy size={12} />
                </button>
              </div>
            </div>
          ) : null}

          <div className="space-y-1">
            {hooks.isLoading ? (
              <div className="text-xs text-muted">Loading…</div>
            ) : (hooks.data ?? []).length === 0 ? (
              <div className="text-xs text-muted">No webhooks yet.</div>
            ) : (
              (hooks.data ?? []).map((h) => (
                <div
                  key={h.id}
                  className="flex items-center gap-2 rounded-md border border-border bg-bg px-2 py-1.5 text-xs"
                >
                  <div className="flex-1 truncate font-mono text-[10px]">{h.url}</div>
                  <span className="text-[10px] text-muted">
                    {h.events.length === 0 ? "all events" : h.events.join(", ")}
                  </span>
                  <button
                    onClick={() => rotate.mutate(h)}
                    className="text-muted hover:text-text"
                    title="Rotate secret"
                  >
                    <RefreshCw size={12} />
                  </button>
                  <button
                    onClick={() => remove.mutate(h.id)}
                    className="text-muted hover:text-priority-urgent"
                    title="Delete webhook"
                  >
                    <X size={12} />
                  </button>
                </div>
              ))
            )}
          </div>
        </>
      )}
    </section>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="mb-3 block">
      <div className="mb-1 text-[10px] font-semibold uppercase tracking-wider text-muted">
        {label}
      </div>
      {children}
    </label>
  );
}
