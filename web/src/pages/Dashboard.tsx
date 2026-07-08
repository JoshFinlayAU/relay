import { useQuery } from "@tanstack/react-query";
import { getStatsOverview } from "../api/messages";
import { Card, PageHeader, StatTile } from "../components/ui";

export default function Dashboard() {
  const { data, isLoading } = useQuery({
    queryKey: ["stats-overview"],
    queryFn: getStatsOverview,
    refetchInterval: 10000,
  });

  const s = data?.by_status ?? {};
  const failed = (s.failed ?? 0) + (s.bounced ?? 0);

  return (
    <div className="space-y-8">
      <PageHeader eyebrow="Overview" title="Dashboard" />

      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {data && (
        <>
          {/* Bento stat grid. */}
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            <StatTile label="Queue depth" value={data.queue_depth} hint="jobs awaiting send" />
            <StatTile label="Delivered" value={s.delivered ?? 0} accent="text-emerald" hint="last 24h" />
            <StatTile label="Deferred" value={s.deferred ?? 0} accent="text-amber" hint="retrying" />
            <StatTile label="Failed / bounced" value={failed} accent="text-rose" hint="last 24h" />
          </div>

          {data.degraded_domains > 0 && (
            <Card className="flex items-center gap-3 border-0 p-4 text-sm text-amber ring-amber/30">
              <span className="h-2 w-2 shrink-0 rounded-full bg-amber" />
              {data.degraded_domains} domain(s) degraded - a DNS record stopped verifying. Check Domains.
            </Card>
          )}

          <div className="space-y-3">
            <h2 className="text-lg font-semibold tracking-tight">Recent activity</h2>
            <Card bezel>
              {data.recent_events.length === 0 ? (
                <p className="p-8 text-center text-sm text-muted-foreground">No activity yet.</p>
              ) : (
                <ul className="divide-y divide-white/[0.05]">
                  {data.recent_events.map((e, i) => (
                    <li key={i} className="flex items-center justify-between px-5 py-3 text-sm">
                      <span className="flex items-center gap-3">
                        <span className="h-1.5 w-1.5 rounded-full bg-primary/70" />
                        <span className="font-mono text-xs text-foreground/90">{e.type}</span>
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {e.created_at ? new Date(e.created_at).toLocaleString() : ""}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          </div>
        </>
      )}
    </div>
  );
}
