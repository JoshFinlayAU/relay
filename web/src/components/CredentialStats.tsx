import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getCredentialStats } from "../api/system";
import { Card } from "./ui";

// Per-credential submitted/delivered/deferred/bounced over a selectable window
// (CLAUDE.md: "per-credential stats charts"). Rendered as tiles plus a stacked
// outcome bar - cheap, no chart bundle needed.
export default function CredentialStats({ credentialId }: { credentialId: string }) {
  const [window, setWindow] = useState("7d");
  const q = useQuery({
    queryKey: ["credential-stats", credentialId, window],
    queryFn: () => getCredentialStats(credentialId, window),
  });
  const s = q.data?.stats;
  const delivered = s?.delivered ?? 0;
  const deferred = s?.deferred ?? 0;
  const bounced = (s?.bounced_hard ?? 0) + (s?.bounced_soft ?? 0);
  const total = delivered + deferred + bounced;
  const pct = (n: number) => (total > 0 ? (n / total) * 100 : 0);

  return (
    <Card className="m-2 p-4">
     <div data-testid="credential-stats" className="space-y-3">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-muted-foreground">Delivery outcomes</span>
        <select
          aria-label="Credential stats window"
          value={window}
          onChange={(e) => setWindow(e.target.value)}
          className="rounded-md bg-white/[0.04] px-2 py-1 text-xs ring-1 ring-inset ring-white/10"
        >
          <option value="24h">24h</option>
          <option value="7d">7d</option>
          <option value="30d">30d</option>
        </select>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Metric label="Submitted" value={s?.submitted ?? 0} />
        <Metric label="Delivered" value={delivered} accent="text-emerald" />
        <Metric label="Deferred" value={deferred} accent="text-amber" />
        <Metric label="Bounced" value={bounced} accent="text-rose" />
      </div>

      {/* Stacked outcome bar. */}
      <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-white/[0.06]" aria-hidden>
        <span className="bg-emerald transition-all duration-500 ease-spring" style={{ width: `${pct(delivered)}%` }} />
        <span className="bg-amber transition-all duration-500 ease-spring" style={{ width: `${pct(deferred)}%` }} />
        <span className="bg-rose transition-all duration-500 ease-spring" style={{ width: `${pct(bounced)}%` }} />
      </div>
      {total === 0 && <p className="text-xs text-muted-foreground">No delivery activity in this window.</p>}
     </div>
    </Card>
  );
}

function Metric({ label, value, accent }: { label: string; value: number; accent?: string }) {
  return (
    <div className="rounded-xl bg-white/[0.03] p-3 ring-1 ring-inset ring-white/[0.06]">
      <div className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className={`mt-1 text-xl font-bold tabular-nums ${accent ?? ""}`}>{value}</div>
    </div>
  );
}
