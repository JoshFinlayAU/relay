import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getDNS, provisionDNS, verifyDomain, type DnsInstruction } from "../api/domains";
import { Button, Card, CheckBadge, CopyButton } from "./ui";
import { ApiError } from "../lib/api";
import { cn } from "../lib/utils";

type Light = "green" | "amber" | "red";

function overall(instr: DnsInstruction[]): { light: Light; label: string } {
  const required = instr.filter((i) => i.required);
  const anyFail = required.some((i) => i.last_result === "fail" || i.last_result === "unknown");
  const anyWarn = instr.some((i) => i.last_result === "warn");
  if (required.length === 0) return { light: "red", label: "Not verified yet" };
  if (anyFail) return { light: "red", label: "Action needed" };
  if (anyWarn) return { light: "amber", label: "Verified - with warnings" };
  return { light: "green", label: "DNS configured" };
}

const lightStyles: Record<Light, { dot: string; ring: string; text: string }> = {
  green: { dot: "bg-emerald", ring: "ring-emerald/30 bg-emerald/10", text: "text-emerald" },
  amber: { dot: "bg-amber", ring: "ring-amber/30 bg-amber/10", text: "text-amber" },
  red: { dot: "bg-rose", ring: "ring-rose/30 bg-rose/10", text: "text-rose" },
};

export default function DnsPanel({ domainId }: { domainId: string }) {
  const qc = useQueryClient();
  const dnsQ = useQuery({ queryKey: ["dns", domainId], queryFn: () => getDNS(domainId) });
  const [open, setOpen] = useState(false);
  const [showCf, setShowCf] = useState(false);

  const invalidate = () => {
    qc.invalidateQueries({ queryKey: ["dns", domainId] });
    qc.invalidateQueries({ queryKey: ["domain", domainId] });
    qc.invalidateQueries({ queryKey: ["domains"] });
  };
  const verify = useMutation({ mutationFn: () => verifyDomain(domainId), onSuccess: invalidate });

  if (!dnsQ.data) return null;
  const instr = dnsQ.data.instructions;
  const status = overall(instr);
  const style = lightStyles[status.light];
  // Auto-expand when action is needed.
  const expanded = open || status.light === "red";

  return (
    <div className="space-y-3">
      {/* Traffic-light summary bar */}
      <Card className={cn("flex flex-wrap items-center justify-between gap-3 p-4 ring-1 ring-inset", style.ring)}>
        <button
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-3 text-left"
          aria-expanded={expanded}
          data-testid="dns-summary"
        >
          <span className={cn("relative flex h-3 w-3", status.light === "green" && "")}>
            <span className={cn("absolute inline-flex h-full w-full rounded-full opacity-60", style.dot, status.light !== "green" && "animate-ping")} />
            <span className={cn("relative inline-flex h-3 w-3 rounded-full", style.dot)} />
          </span>
          <span>
            <span className={cn("font-semibold", style.text)}>{status.label}</span>
            <span className="ml-2 text-sm text-muted-foreground">
              {status.light === "green" ? "All required DNS records verified." : "DNS records for this domain"}
            </span>
          </span>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"
            className={cn("text-muted-foreground transition-transform duration-300 ease-spring", expanded && "rotate-180")}>
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => setShowCf((v) => !v)}>Auto-configure</Button>
          <Button onClick={() => verify.mutate()} disabled={verify.isPending} data-testid="verify-now">
            {verify.isPending ? "Verifying…" : "Verify now"}
          </Button>
        </div>
      </Card>

      {showCf && <CloudflarePanel domainId={domainId} onDone={invalidate} onClose={() => setShowCf(false)} />}

      {verify.isSuccess && (
        <p data-testid="verify-result" className="text-sm text-muted-foreground">
          {verify.data.active ? "All required records verified - domain is active." : "Some required records aren’t published/correct yet."}
        </p>
      )}

      {expanded && (
        <div className="space-y-3">
          {dnsQ.data.operator_note && (
            <p className="rounded-xl bg-white/[0.03] p-3 text-xs text-muted-foreground ring-1 ring-inset ring-white/[0.06]">
              {dnsQ.data.operator_note}
            </p>
          )}
          {instr.map((r) => (
            <Card key={r.purpose} className="p-4">
              <div className="mb-2 flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="font-semibold capitalize">{r.purpose.replace("_", " ")}</span>
                  <span className="rounded bg-white/[0.06] px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">{r.type}</span>
                  {!r.required && <span className="text-xs text-muted-foreground">recommended</span>}
                </div>
                <CheckBadge result={r.last_result} />
              </div>
              <div className="space-y-1 font-mono text-xs">
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">name</span>
                  <span className="break-all">{r.name}</span>
                </div>
                <div className="flex items-start gap-2">
                  <span className="text-muted-foreground">value</span>
                  <span className="break-all text-foreground/90">{r.value}</span>
                  <CopyButton text={r.value} />
                </div>
              </div>
              {r.conflict && r.merged_value && (
                <div className="mt-2 rounded-lg bg-amber/10 p-2 text-xs ring-1 ring-inset ring-amber/30">
                  <div className="mb-1 font-medium text-amber">Existing SPF found - replace it with this merged value:</div>
                  <div className="flex items-start gap-2 font-mono">
                    <span className="break-all">{r.merged_value}</span>
                    <CopyButton text={r.merged_value} />
                  </div>
                </div>
              )}
              {r.detail && r.last_result !== "pass" && <p className="mt-2 text-xs text-muted-foreground">{r.detail}</p>}
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}

function CloudflarePanel({ domainId, onDone, onClose }: { domainId: string; onDone: () => void; onClose: () => void }) {
  const [token, setToken] = useState("");
  const mut = useMutation({
    mutationFn: () => provisionDNS(domainId, token.trim()),
    onSuccess: onDone,
  });
  return (
    <Card bezel>
      <div className="space-y-3 p-5">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold">Auto-configure via Cloudflare</h3>
          <button onClick={onClose} className="text-sm text-muted-foreground hover:text-foreground">Close</button>
        </div>
        <p className="text-sm text-muted-foreground">
          Create a token with Cloudflare’s <span className="font-mono text-xs">Edit zone DNS</span> template — it grants
          both <span className="font-mono text-xs">Zone:Read</span> and <span className="font-mono text-xs">DNS:Edit</span>.
          A DNS-Edit-only token can’t look up the zone and will be rejected. Relay creates every record (merging any
          existing SPF); the token is used once and never stored.
        </p>
        <a
          href="https://dash.cloudflare.com/profile/api-tokens"
          target="_blank"
          rel="noreferrer"
          className="inline-block text-sm text-primary hover:underline"
        >
          Create a token in Cloudflare (use the “Edit zone DNS” template) →
        </a>
        <div className="flex gap-2">
          <input
            aria-label="Cloudflare API token"
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Cloudflare API token"
            className="flex-1 rounded-xl bg-background/60 px-3.5 py-2.5 text-sm outline-none ring-1 ring-inset ring-white/10 focus:ring-2 focus:ring-primary/70"
          />
          <Button onClick={() => mut.mutate()} disabled={mut.isPending || token.trim().length < 10}>
            {mut.isPending ? "Configuring…" : "Configure DNS"}
          </Button>
        </div>
        {mut.isError && <p role="alert" className="text-sm text-rose">{(mut.error as ApiError).message}</p>}
        {mut.isSuccess && (
          <div className="space-y-1 text-sm">
            <p className="text-emerald">Done - {mut.data.results.length} records processed. Verify to confirm.</p>
            <ul className="font-mono text-xs text-muted-foreground">
              {mut.data.results.map((r, i) => (
                <li key={i} className={r.action === "failed" ? "text-rose" : ""}>
                  {r.purpose}: {r.action}{r.error ? ` - ${r.error}` : ""}
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </Card>
  );
}
