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

const EMPTY = { status: "", direction: "", from: "", subject: "", rcpt: "", after: "", before: "" };
type Filters = typeof EMPTY;

const inputCls =
  "rounded-md border border-border bg-background px-3 py-1.5 text-sm outline-none focus:border-primary";

export default function Messages() {
  // `draft` is what the user is typing; `active` is what we actually query, so
  // typing doesn't fire a request per keystroke.
  const [draft, setDraft] = useState<Filters>(EMPTY);
  const [active, setActive] = useState<Filters>(EMPTY);

  const params: Record<string, string> = {};
  if (active.status) params.status = active.status;
  if (active.direction) params.direction = active.direction;
  if (active.from.trim()) params.from = active.from.trim();
  if (active.subject.trim()) params.subject = active.subject.trim();
  if (active.rcpt.trim()) params.rcpt = active.rcpt.trim();
  if (active.after) params.after = `${active.after}T00:00:00Z`;
  if (active.before) params.before = `${active.before}T23:59:59Z`;

  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["messages", active],
    queryFn: ({ pageParam }) => listMessages(pageParam ? { ...params, cursor: pageParam } : params),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const messages = data?.pages.flatMap((p) => p.messages) ?? [];
  const activeCount = Object.values(active).filter(Boolean).length;

  const set = (patch: Partial<Filters>) => setDraft({ ...draft, ...patch });
  const search = (e: React.FormEvent) => {
    e.preventDefault();
    setActive(draft);
  };
  const reset = () => {
    setDraft(EMPTY);
    setActive(EMPTY);
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Messages</h1>

      {/* Detailed search */}
      <Card className="p-4">
        <form onSubmit={search} className="space-y-3">
          <div className="grid gap-3 md:grid-cols-3">
            <input aria-label="Search sender" placeholder="From (sender)" value={draft.from}
              onChange={(e) => set({ from: e.target.value })} className={inputCls} />
            <input aria-label="Search recipient" placeholder="To (recipient)" value={draft.rcpt}
              onChange={(e) => set({ rcpt: e.target.value })} className={inputCls} />
            <input aria-label="Search subject" placeholder="Subject contains…" value={draft.subject}
              onChange={(e) => set({ subject: e.target.value })} className={inputCls} />
            <select aria-label="Filter status" value={draft.status}
              onChange={(e) => set({ status: e.target.value })} className={inputCls}>
              <option value="">All statuses</option>
              {["queued", "delivered", "deferred", "partial", "failed", "bounced"].map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
            <select aria-label="Filter direction" value={draft.direction}
              onChange={(e) => set({ direction: e.target.value })} className={inputCls}>
              <option value="">Both directions</option>
              <option value="outbound">Outbound</option>
              <option value="inbound">Inbound</option>
            </select>
            <div className="flex items-center gap-2">
              <input type="date" aria-label="From date" value={draft.after}
                onChange={(e) => set({ after: e.target.value })} className={cn(inputCls, "flex-1")} />
              <span className="text-muted-foreground">→</span>
              <input type="date" aria-label="To date" value={draft.before}
                onChange={(e) => set({ before: e.target.value })} className={cn(inputCls, "flex-1")} />
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button type="submit" data-testid="search-messages">Search</Button>
            {activeCount > 0 && (
              <Button type="button" variant="ghost" onClick={reset}>Reset</Button>
            )}
            <span className="ml-auto text-xs text-muted-foreground">
              {messages.length}{hasNextPage ? "+" : ""} result{messages.length === 1 ? "" : "s"}
              {activeCount > 0 ? ` · ${activeCount} filter${activeCount === 1 ? "" : "s"}` : ""}
            </span>
          </div>
        </form>
      </Card>

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
