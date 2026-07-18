import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { getDMARC } from "../api/dmarc";
import { Card, StatTile } from "./ui";
import { cn } from "../lib/utils";

export default function DmarcPanel({ domainId }: { domainId: string }) {
  const [window, setWindow] = useState("30d");
  const { data } = useQuery({ queryKey: ["dmarc", domainId, window], queryFn: () => getDMARC(domainId, window) });

  const s = data?.summary;
  const total = s?.total ?? 0;
  const passRate = total > 0 ? Math.round(((s?.passed ?? 0) / total) * 100) : null;

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">DMARC</h2>
        <select
          aria-label="DMARC window"
          value={window}
          onChange={(e) => setWindow(e.target.value)}
          className="rounded-md border border-border bg-background px-2 py-1 text-sm"
        >
          <option value="7d">7d</option>
          <option value="30d">30d</option>
          <option value="90d">90d</option>
        </select>
      </div>

      {total === 0 ? (
        <Card className="p-6 text-sm text-muted-foreground">
          No DMARC aggregate reports yet for this window. Publish the domain&apos;s DMARC record
          (its <span className="font-mono text-xs">rua</span> points at this server) and reports will
          appear here as mailbox providers send them — typically once per day.
        </Card>
      ) : (
        <>
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <StatTile label="Messages" value={total} />
            <StatTile label="DMARC pass" value={passRate == null ? "-" : `${passRate}%`}
              accent={passRate != null && passRate >= 95 ? "text-emerald" : "text-amber"} />
            <StatTile label="Quarantined" value={s?.quarantined ?? 0} accent="text-amber" />
            <StatTile label="Rejected" value={s?.rejected ?? 0} accent="text-rose" />
          </div>

          {/* Alignment breakdown */}
          <Card className="p-4 text-sm">
            <div className="mb-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">Alignment</div>
            <div className="flex gap-6">
              <span>DKIM pass: <span className="font-semibold text-emerald">{s?.dkim_pass ?? 0}</span></span>
              <span>SPF pass: <span className="font-semibold text-emerald">{s?.spf_pass ?? 0}</span></span>
              <span>Failing both: <span className="font-semibold text-rose">{total - (s?.passed ?? 0)}</span></span>
            </div>
          </Card>

          {data && data.top_sources.length > 0 && (
            <Card>
              <div className="border-b border-border px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Top sending sources
              </div>
              <table className="w-full text-sm">
                <tbody>
                  {data.top_sources.map((src) => {
                    const pct = src.total > 0 ? Math.round((src.passed / src.total) * 100) : 0;
                    return (
                      <tr key={src.source_ip} className="border-b border-border last:border-0">
                        <td className="px-4 py-2 font-mono text-xs">{src.source_ip}</td>
                        <td className="px-4 py-2 text-right text-muted-foreground">{src.total} msgs</td>
                        <td className={cn("px-4 py-2 text-right font-medium", pct >= 95 ? "text-emerald" : pct >= 50 ? "text-amber" : "text-rose")}>
                          {pct}% pass
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </Card>
          )}

          {data && data.reports.length > 0 && (
            <Card>
              <div className="border-b border-border px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground">
                Recent reports
              </div>
              <table className="w-full text-sm">
                <thead className="text-left text-muted-foreground">
                  <tr>
                    <th className="px-4 py-2 font-medium">Reporter</th>
                    <th className="px-4 py-2 font-medium">Period</th>
                    <th className="px-4 py-2 font-medium">Policy</th>
                    <th className="px-4 py-2 text-right font-medium">Messages</th>
                  </tr>
                </thead>
                <tbody>
                  {data.reports.map((rp) => (
                    <tr key={rp.report_id} className="border-b border-border last:border-0" data-testid="dmarc-report">
                      <td className="px-4 py-2">{rp.org_name}</td>
                      <td className="px-4 py-2 text-muted-foreground">
                        {rp.date_end ? new Date(rp.date_end).toLocaleDateString() : "-"}
                      </td>
                      <td className="px-4 py-2 font-mono text-xs">p={rp.policy_p ?? "-"}{rp.policy_pct != null ? ` pct=${rp.policy_pct}` : ""}</td>
                      <td className="px-4 py-2 text-right">{rp.messages}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          )}
        </>
      )}
    </div>
  );
}
