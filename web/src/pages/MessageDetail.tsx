import { useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getMessage, getMessageRaw } from "../api/messages";
import { Button, Card } from "../components/ui";
import { cn } from "../lib/utils";

const resultColor: Record<string, string> = {
  delivered: "border-green-500/30 bg-green-500/10 text-green-400",
  deferred: "border-orange-500/30 bg-orange-500/10 text-orange-400",
  failed: "border-red-500/30 bg-red-500/10 text-red-400",
};

export default function MessageDetail() {
  const { id = "" } = useParams();
  const { data, isLoading, isError } = useQuery({
    queryKey: ["message", id],
    queryFn: () => getMessage(id),
  });

  if (isLoading) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (isError || !data) return <p role="alert" className="text-sm text-destructive">Message not found.</p>;

  const m = data.message;
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">{m.subject || "(no subject)"}</h1>

      <Card className="grid grid-cols-2 gap-x-8 gap-y-2 p-4 text-sm">
        <Field label="Status" value={m.status} />
        <Field label="Direction" value={m.direction} />
        <Field label="Header From" value={m.header_from ?? "-"} mono />
        <Field label="Return-Path (VERP)" value={m.mail_from ?? "-"} mono />
        <Field label="Recipients" value={m.rcpt_to.join(", ")} mono />
        <Field label="DKIM selector" value={m.dkim_selector ?? "-"} mono />
        <Field label="Size" value={`${m.size_bytes} bytes`} />
        <Field label="Created" value={m.created_at ? new Date(m.created_at).toLocaleString() : "-"} />
      </Card>

      <div className="space-y-3">
        <h2 className="text-lg font-semibold">Delivery timeline</h2>
        {data.attempts.length === 0 && (
          <p className="text-sm text-muted-foreground">No delivery attempts recorded yet.</p>
        )}
        {data.attempts.map((a, i) => (
          <Card key={i} className="p-4" data-testid="attempt">
            <div className="mb-2 flex items-center justify-between">
              <span className="font-mono text-xs">{a.rcpt}</span>
              <span className={cn("rounded-full border px-2 py-0.5 text-xs font-medium capitalize", resultColor[a.result] ?? "")}>
                {a.result}
              </span>
            </div>
            <div className="space-y-1 font-mono text-xs text-muted-foreground">
              {a.mx_host && <div>MX: {a.mx_host}</div>}
              {(a.smtp_code || a.smtp_response) && (
                <div>SMTP: {a.smtp_code} {a.smtp_response}</div>
              )}
              {a.tls_version && (
                <div>TLS: {a.tls_version} {a.tls_verified ? "(verified)" : "(unverified)"}</div>
              )}
              {a.started_at && <div>At: {new Date(a.started_at).toLocaleString()}</div>}
            </div>
          </Card>
        ))}
      </div>

      <RawHeaders id={id} />

      {data.bounces.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-lg font-semibold">Bounces &amp; complaints</h2>
          {data.bounces.map((b, i) => (
            <Card key={i} className="p-4" data-testid="bounce">
              <div className="flex items-center justify-between">
                <span className="font-mono text-xs">{b.rcpt ?? "-"}</span>
                <span
                  className={cn(
                    "rounded-full border px-2 py-0.5 text-xs font-medium capitalize",
                    b.type === "hard" || b.type === "complaint"
                      ? "border-red-500/30 bg-red-500/10 text-red-400"
                      : "border-orange-500/30 bg-orange-500/10 text-orange-400",
                  )}
                >
                  {b.type}
                </span>
              </div>
              {b.dsn_code && <div className="mt-1 font-mono text-xs text-muted-foreground">DSN {b.dsn_code}</div>}
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}

function RawHeaders({ id }: { id: string }) {
  const [open, setOpen] = useState(false);
  const q = useQuery({
    queryKey: ["message-raw", id],
    queryFn: () => getMessageRaw(id),
    enabled: open, // fetch only when the operator opens it
    retry: false,
  });
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Raw headers</h2>
        <Button variant="secondary" data-testid="raw-headers-toggle" onClick={() => setOpen((v) => !v)}>
          {open ? "Hide" : "View raw headers"}
        </Button>
      </div>
      {open && (
        <Card className="p-4">
          {q.isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
          {q.isError && <p className="text-sm text-muted-foreground">{(q.error as Error).message}</p>}
          {q.data !== undefined && (
            // Rendered as plain text inside <pre> - never as HTML (no XSS).
            <pre className="max-h-96 overflow-auto whitespace-pre-wrap break-all font-mono text-xs text-foreground/90">
              {q.data}
            </pre>
          )}
        </Card>
      )}
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn("break-all", mono && "font-mono text-xs")}>{value}</div>
    </div>
  );
}
