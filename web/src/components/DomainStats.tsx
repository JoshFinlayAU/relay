import { lazy, Suspense, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { getDomainStats, getDomainTimeseries, testSend } from "../api/system";
import { Button, Card, StatTile } from "./ui";
import { ApiError } from "../lib/api";

const StatsAreaChart = lazy(() => import("./StatsAreaChart"));

export default function DomainStats({ domainId }: { domainId: string }) {
  const [window, setWindow] = useState("24h");
  const statsQ = useQuery({ queryKey: ["domain-stats", domainId, window], queryFn: () => getDomainStats(domainId, window) });
  const seriesQ = useQuery({ queryKey: ["domain-ts", domainId], queryFn: () => getDomainTimeseries(domainId, "7d") });

  const [to, setTo] = useState("");
  const [sent, setSent] = useState<string | null>(null);
  const send = useMutation({
    mutationFn: () => testSend(domainId, to.trim()),
    onSuccess: (r) => setSent(r.message_id),
  });

  const s = statsQ.data?.stats;
  const chartData = (seriesQ.data?.buckets ?? []).map((b) => ({
    t: b.bucket ? new Date(b.bucket).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit" }) : "",
    delivered: b.delivered,
    deferred: b.deferred,
    bounced: b.bounced_hard + b.bounced_soft,
  }));

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Statistics</h2>
        <select
          aria-label="Stats window"
          value={window}
          onChange={(e) => setWindow(e.target.value)}
          className="rounded-md border border-border bg-background px-2 py-1 text-sm"
        >
          <option value="24h">24h</option>
          <option value="7d">7d</option>
          <option value="30d">30d</option>
        </select>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatTile label="Submitted" value={s?.submitted ?? 0} />
        <StatTile label="Delivered" value={s?.delivered ?? 0} accent="text-emerald" />
        <StatTile label="Deferred" value={s?.deferred ?? 0} accent="text-amber" />
        <StatTile label="Bounced" value={(s?.bounced_hard ?? 0) + (s?.bounced_soft ?? 0)} accent="text-rose" />
      </div>

      {chartData.length > 0 && (
        <Card className="p-4">
          <div className="mb-2 text-xs text-muted-foreground">Last 7 days (hourly)</div>
          <Suspense fallback={<div className="h-[220px] animate-pulse rounded-xl bg-white/[0.03]" />}>
            <StatsAreaChart data={chartData} />
          </Suspense>
        </Card>
      )}

      <Card className="p-4">
        <div className="mb-2 text-sm font-medium">Send a test message</div>
        <form
          onSubmit={(e) => { e.preventDefault(); send.mutate(); }}
          className="flex gap-2"
        >
          <input
            aria-label="Test recipient"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            placeholder="you@example.com"
            className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
          />
          <Button type="submit" disabled={send.isPending || !to.includes("@")} data-testid="test-send">
            {send.isPending ? "Sending…" : "Test send"}
          </Button>
        </form>
        {send.isError && <p role="alert" className="mt-2 text-sm text-destructive">{(send.error as ApiError).message}</p>}
        {sent && (
          <p className="mt-2 text-sm text-muted-foreground">
            Queued - trace at <a className="text-primary hover:underline" href={`/messages/${sent}`}>/messages/{sent.slice(0, 8)}</a>
          </p>
        )}
      </Card>
    </div>
  );
}

