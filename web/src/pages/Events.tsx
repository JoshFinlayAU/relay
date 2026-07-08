import { useInfiniteQuery } from "@tanstack/react-query";
import { listEvents } from "../api/system";
import { Button, Card } from "../components/ui";

export default function Events() {
  const { data, isLoading, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["events"],
    queryFn: ({ pageParam }) => listEvents(pageParam ? { limit: "100", cursor: pageParam } : { limit: "100" }),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
  const events = data?.pages.flatMap((p) => p.events) ?? [];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Events</h1>
      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {!isLoading && events.length === 0 && (
        <Card className="p-8 text-center text-sm text-muted-foreground">No events yet.</Card>
      )}
      {events.length > 0 && (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-3 font-medium">When</th>
                <th className="px-4 py-3 font-medium">Type</th>
                <th className="px-4 py-3 font-medium">Detail</th>
              </tr>
            </thead>
            <tbody>
              {events.map((e, i) => (
                <tr key={i} className="border-b border-border last:border-0" data-testid="event-row">
                  <td className="whitespace-nowrap px-4 py-2 text-muted-foreground">
                    {e.created_at ? new Date(e.created_at).toLocaleString() : ""}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs">{e.type}</td>
                  <td className="px-4 py-2 font-mono text-xs text-muted-foreground">
                    {e.detail && Object.keys(e.detail as object).length > 0 ? JSON.stringify(e.detail) : ""}
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
