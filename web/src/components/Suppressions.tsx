import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { addSuppression, listSuppressions, removeSuppression } from "../api/suppressions";
import { Button, Card } from "./ui";
import { ApiError } from "../lib/api";

export default function Suppressions({ domainId }: { domainId: string }) {
  const [address, setAddress] = useState("");
  const qc = useQueryClient();
  const { data } = useQuery({
    queryKey: ["suppressions", domainId],
    queryFn: () => listSuppressions(domainId),
  });

  const addMut = useMutation({
    mutationFn: () => addSuppression(domainId, address.trim(), "manual"),
    onSuccess: () => {
      setAddress("");
      qc.invalidateQueries({ queryKey: ["suppressions", domainId] });
    },
  });
  const rmMut = useMutation({
    mutationFn: (addr: string) => removeSuppression(domainId, addr),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["suppressions", domainId] }),
  });

  return (
    <div className="space-y-3">
      <h2 className="text-lg font-semibold">Suppressed addresses</h2>
      <p className="text-sm text-muted-foreground">
        Hard bounces and complaints land here automatically and are rejected at submission
        (550 5.1.1). Remove an address to override.
      </p>

      <form
        onSubmit={(e) => { e.preventDefault(); addMut.mutate(); }}
        className="flex gap-2"
      >
        <input
          aria-label="Suppress address"
          value={address}
          onChange={(e) => setAddress(e.target.value)}
          placeholder="user@example.com"
          className="flex-1 rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
        />
        <Button type="submit" disabled={addMut.isPending || !address.includes("@")}>Suppress</Button>
      </form>
      {addMut.isError && <p role="alert" className="text-sm text-destructive">{(addMut.error as ApiError).message}</p>}

      {data && data.suppressions.length === 0 && (
        <Card className="p-6 text-center text-sm text-muted-foreground">No suppressed addresses.</Card>
      )}
      {data && data.suppressions.length > 0 && (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium">Address</th>
                <th className="px-4 py-2 font-medium">Reason</th>
                <th className="px-4 py-2 font-medium">Since</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {data.suppressions.map((s) => (
                <tr key={s.address} className="border-b border-border last:border-0" data-testid="suppression-row">
                  <td className="px-4 py-2 font-mono text-xs">{s.address}</td>
                  <td className="px-4 py-2 text-muted-foreground">{s.reason ?? "-"}</td>
                  <td className="px-4 py-2 text-muted-foreground">
                    {s.created_at ? new Date(s.created_at).toLocaleString() : ""}
                  </td>
                  <td className="px-4 py-2 text-right">
                    <Button variant="ghost" className="text-xs" onClick={() => rmMut.mutate(s.address)}>
                      Remove
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  );
}
