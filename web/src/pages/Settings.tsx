import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getServerInfo } from "../api/system";
import { getRetention, setRetention, type RetentionPolicy } from "../api/settings";
import { getServerTLS, reloadServerTLS } from "../api/tls";
import { Button, Card, CopyButton } from "../components/ui";
import { ApiError } from "../lib/api";
import { cn } from "../lib/utils";

export default function Settings() {
  const { data, isLoading } = useQuery({ queryKey: ["server-info"], queryFn: getServerInfo, refetchInterval: 15000 });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Settings</h1>
      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {data && (
        <>
          <Card className="grid grid-cols-2 gap-x-8 gap-y-3 p-5 text-sm md:grid-cols-3">
            <Field label="Hostname" value={data.hostname} mono />
            <Field label="Version" value={data.version} />
            <Field label="Database" value={data.database.ok ? "connected" : "down"} accent={data.database.ok ? "text-green-400" : "text-red-400"} />
            <Field label="Queue depth" value={String(data.queue_depth)} />
            <Field label="Sending IPv4" value={data.sending_ipv4} mono />
            <Field label="Sending IPv6" value={data.sending_ipv6} mono />
          </Card>

          <div>
            <h2 className="mb-2 text-lg font-semibold">Server DNS</h2>
            <Card className="space-y-3 p-5 text-sm">
              <p className="text-muted-foreground">
                Publish these for the mail host itself so it can send/receive and pass SPF. These
                back the SPF <span className="font-mono text-xs">include:{data.spf_include}</span> that
                domains onboarded here reference.
              </p>
              {(!data.sending_ipv4 || !data.sending_ipv6) && (
                <p className="rounded-lg border border-orange-500/30 bg-orange-500/10 p-3 text-xs text-orange-300">
                  {!data.sending_ipv4 && !data.sending_ipv6
                    ? "No public sending IP was detected."
                    : `Only ${data.sending_ipv4 ? "IPv4" : "IPv6"} was detected.`}{" "}
                  If this host is behind NAT/port-forwarding (or has multiple IPv6 addresses), set{" "}
                  <span className="font-mono">sending_ipv4</span> / <span className="font-mono">sending_ipv6</span> in
                  <span className="font-mono"> relay.toml</span> to your public (internet-facing) address and restart —
                  SPF and the records below depend on it.
                </p>
              )}
              {data.server_dns.length > 0 && (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead className="border-b border-border text-left text-muted-foreground">
                      <tr>
                        <th className="px-2 py-2 font-medium">Type</th>
                        <th className="px-2 py-2 font-medium">Name</th>
                        <th className="px-2 py-2 font-medium">Value</th>
                        <th className="px-2 py-2" />
                      </tr>
                    </thead>
                    <tbody>
                      {data.server_dns.map((r, i) => (
                        <tr key={i} className="border-b border-border last:border-0">
                          <td className="px-2 py-2 font-mono text-xs">{r.type}</td>
                          <td className="px-2 py-2 font-mono text-xs break-all">{r.name}</td>
                          <td className="px-2 py-2 font-mono text-xs break-all">{r.value}</td>
                          <td className="px-2 py-2 text-right"><CopyButton text={r.value} /></td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
              <p className="text-xs text-muted-foreground">
                Also required (set with your IP/hosting provider, not in your DNS zone):
                reverse DNS (PTR) for {data.sending_ipv4 || "your public IPv4"}
                {data.sending_ipv6 ? ` and ${data.sending_ipv6}` : ""} must resolve to{" "}
                <span className="font-mono">{data.ptr_expected}</span>.
              </p>
            </Card>
          </div>

          <ServerTLSSettings tlsEnabled={data.tls_enabled} />

          <div>
            <h2 className="mb-2 text-lg font-semibold">Listeners</h2>
            <Card className="p-5">
              <table className="w-full text-sm">
                <tbody>
                  {Object.entries(data.listeners).map(([name, addr]) => (
                    <tr key={name} className="border-b border-border last:border-0">
                      <td className="px-2 py-2 capitalize text-muted-foreground">{name}</td>
                      <td className="px-2 py-2 font-mono">{addr}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          </div>

          <RetentionSettings />

          <p className="text-xs text-muted-foreground">
            Admin accounts are managed under <span className="font-medium">Admin users</span>. API
            automation uses static bearer tokens configured on the server.
          </p>
        </>
      )}
    </div>
  );
}

const TLS_SOURCE_LABEL: Record<string, string> = {
  acme: "Let's Encrypt (automatic)",
  "manual-file": "Manual (tls_cert_file / tls_key_file)",
  "self-signed": "Self-signed (dev)",
  disabled: "Disabled",
};

function ServerTLSSettings({ tlsEnabled }: { tlsEnabled: boolean }) {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["server-tls"], queryFn: getServerTLS });
  const reload = useMutation({
    mutationFn: reloadServerTLS,
    onSuccess: (res) => {
      qc.setQueryData(["server-tls"], res);
      qc.invalidateQueries({ queryKey: ["server-info"] });
    },
  });
  const src = data?.source ?? (tlsEnabled ? "acme" : "disabled");
  return (
    <div>
      <h2 className="mb-2 text-lg font-semibold">TLS certificate (server hostname)</h2>
      <Card className="space-y-3 p-5 text-sm">
        <div className="grid grid-cols-2 gap-x-8 gap-y-2 md:grid-cols-3">
          <Field label="Source" value={TLS_SOURCE_LABEL[src] ?? src}
            accent={src === "self-signed" ? "text-orange-400" : "text-green-400"} />
          {data?.not_after && <Field label="Expires" value={new Date(data.not_after).toLocaleDateString()} />}
          {typeof data?.days_remaining === "number" && (
            <Field label="Days remaining" value={String(data.days_remaining)}
              accent={data.days_remaining < 14 ? "text-orange-400" : undefined} />
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          Set via <span className="font-mono">acme_enabled</span> /{" "}
          <span className="font-mono">tls_cert_file</span> /{" "}
          <span className="font-mono">tls_key_file</span> in relay.toml. After swapping renewed cert
          files, hot-reload them here — no restart, no downtime.
        </p>
        <div className="flex items-center gap-3">
          <Button variant="secondary" onClick={() => reload.mutate()} disabled={reload.isPending} data-testid="reload-tls">
            {reload.isPending ? "Reloading…" : "Reload certificates"}
          </Button>
          {reload.isSuccess && <span className="text-xs text-green-400">Reloaded.</span>}
          {reload.isError && <span className="text-xs text-destructive">Reload failed.</span>}
        </div>
      </Card>
    </div>
  );
}

function RetentionSettings() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["retention"], queryFn: getRetention });
  const [form, setForm] = useState<RetentionPolicy | null>(null);

  // Seed the local form once the policy loads.
  useEffect(() => {
    if (data && !form) setForm(data.policy);
  }, [data, form]);

  const mut = useMutation({
    mutationFn: (p: RetentionPolicy) => setRetention(p),
    onSuccess: (res) => {
      qc.setQueryData(["retention"], res);
      setForm(res.policy);
    },
  });

  if (!form) return null;
  const set = (patch: Partial<RetentionPolicy>) => setForm({ ...form, ...patch });

  return (
    <div>
      <h2 className="mb-2 text-lg font-semibold">Message retention</h2>
      <Card className="space-y-4 p-5 text-sm">
        <p className="text-muted-foreground">
          How long delivered/received mail and its delivery history are kept. Stored message bodies
          are always reaped sooner; this controls the message + delivery records.
        </p>

        <label className="flex items-center gap-3">
          <input
            type="checkbox"
            aria-label="Retention enabled"
            checked={form.enabled}
            onChange={(e) => set({ enabled: e.target.checked })}
          />
          <span>Automatically prune old messages</span>
        </label>

        <fieldset disabled={!form.enabled} className={cn("space-y-3", !form.enabled && "opacity-50")}>
          <div className="flex flex-wrap gap-4">
            <label className="flex items-center gap-2">
              <input
                type="radio"
                name="retention-mode"
                aria-label="Keep by age"
                checked={form.mode === "age"}
                onChange={() => set({ mode: "age" })}
              />
              <span>Keep by age</span>
            </label>
            <label className="flex items-center gap-2">
              <input
                type="radio"
                name="retention-mode"
                aria-label="Keep by count"
                checked={form.mode === "count"}
                onChange={() => set({ mode: "count" })}
              />
              <span>Keep newest N messages</span>
            </label>
          </div>

          {form.mode === "age" ? (
            <label className="flex items-center gap-2">
              Keep the last
              <input
                type="number"
                min={1}
                aria-label="Retention days"
                value={form.days}
                onChange={(e) => set({ days: Number(e.target.value) })}
                className="w-24 rounded-md border border-border bg-background px-2 py-1"
              />
              days of mail
            </label>
          ) : (
            <label className="flex items-center gap-2">
              Keep the newest
              <input
                type="number"
                min={1}
                aria-label="Retention max messages"
                value={form.max_messages}
                onChange={(e) => set({ max_messages: Number(e.target.value) })}
                className="w-32 rounded-md border border-border bg-background px-2 py-1"
              />
              messages
            </label>
          )}
        </fieldset>

        {mut.isError && <p role="alert" className="text-destructive">{(mut.error as ApiError).message}</p>}
        <div className="flex items-center gap-3">
          <Button onClick={() => mut.mutate(form)} disabled={mut.isPending} data-testid="save-retention">
            {mut.isPending ? "Saving…" : "Save retention"}
          </Button>
          {mut.isSuccess && <span className="text-xs text-green-400">Saved.</span>}
          {data?.source === "default" && !mut.isSuccess && (
            <span className="text-xs text-muted-foreground">Using server default (not yet customised).</span>
          )}
        </div>
      </Card>
    </div>
  );
}

function Field({ label, value, mono, accent }: { label: string; value: string; mono?: boolean; accent?: string }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn("mt-0.5", mono && "font-mono text-xs", accent)}>{value}</div>
    </div>
  );
}
