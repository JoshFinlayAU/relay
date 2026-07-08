import { useState } from "react";
import { Link } from "react-router-dom";
import { useInfiniteQuery } from "@tanstack/react-query";
import { listMessages } from "../api/messages";
import { Button, Card } from "../components/ui";
import { cn } from "../lib/utils";

const statusColor: Record<string, string> = {
  delivered: "text-green-400",
  queued: "text-yellow-400",
  deferred: "text-orange-400",
  partial: "text-orange-400",
  failed: "text-red-400",
  bounced: "text-red-400",
};

export default function Messages() {
  const [status, setStatus] = useState("");
  const [direction, setDirection] = useState("");
  const [rcpt, setRcpt] = useState("");

  const params: Record<string, string> = {};
  if (status) params.status = status;
  if (direction) params.direction = direction;
  if (rcpt.trim()) params.rcpt = rcpt.trim();

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["messages", status, direction, rcpt],
    queryFn: ({ pageParam }) => listMessages(pageParam ? { ...params, cursor: pageParam } : params),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const messages = data?.pages.flatMap((p) => p.messages) ?? [];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Messages</h1>

      <div className="flex gap-3">
        <select
          aria-label="Filter status"
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          className="rounded-md border border-border bg-background px-3 py-1.5 text-sm"
        >
          <option value="">All statuses</option>
          {["queued", "delivered", "deferred", "partial", "failed", "bounced"].map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <select
          aria-label="Filter direction"
          value={direction}
          onChange={(e) => setDirection(e.target.value)}
          className="rounded-md border border-border bg-background px-3 py-1.5 text-sm"
        >
          <option value="">Both directions</option>
          <option value="outbound">Outbound</option>
          <option value="inbound">Inbound</option>
        </select>
        <input
          aria-label="Search recipient"
          value={rcpt}
          onChange={(e) => setRcpt(e.target.value)}
          placeholder="recipient@example.com"
          className="flex-1 rounded-md border border-border bg-background px-3 py-1.5 text-sm outline-none focus:border-primary"
        />
      </div>

      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {!isLoading && messages.length === 0 && (
        <Card className="p-8 text-center text-sm text-muted-foreground">No messages match.</Card>
      )}
      {messages.length > 0 && (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-3 font-medium">Subject</th>
                <th className="px-4 py-3 font-medium">From</th>
                <th className="px-4 py-3 font-medium">To</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">When</th>
              </tr>
            </thead>
            <tbody>
              {messages.map((m) => (
                <tr key={m.id} className="border-b border-border last:border-0" data-testid="message-row">
                  <td className="px-4 py-3">
                    <Link to={`/messages/${m.id}`} className="text-primary hover:underline">
                      {m.subject || "(no subject)"}
                    </Link>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs">{m.header_from}</td>
                  <td className="px-4 py-3 font-mono text-xs">{m.rcpt_to.join(", ")}</td>
                  <td className={cn("px-4 py-3 font-medium", statusColor[m.status] ?? "")}>{m.status}</td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {m.created_at ? new Date(m.created_at).toLocaleString() : ""}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
      {hasNextPage && (
        <div className="flex justify-center">
          <Button variant="secondary" data-testid="load-more" onClick={() => fetchNextPage()} disabled={isFetchingNextPage}>
            {isFetchingNextPage ? "Loading…" : "Load more"}
          </Button>
        </div>
      )}
    </div>
  );
}
