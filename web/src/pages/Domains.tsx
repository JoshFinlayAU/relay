import { useState } from "react";
import { Link } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createDomain, listDomains } from "../api/domains";
import { Button, Card, StatusBadge } from "../components/ui";
import { ApiError } from "../lib/api";

export default function Domains() {
  const [showAdd, setShowAdd] = useState(false);
  const { data, isLoading, isError, error } = useQuery({
    queryKey: ["domains"],
    queryFn: listDomains,
  });

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Domains</h1>
        <Button onClick={() => setShowAdd(true)}>Add domain</Button>
      </div>

      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {isError && (
        <p role="alert" className="text-sm text-destructive">
          {(error as Error).message}
        </p>
      )}

      {data && data.domains.length === 0 && (
        <Card className="p-8 text-center text-sm text-muted-foreground">
          No domains yet. Add one to get onboarding DNS records.
        </Card>
      )}

      {data && data.domains.length > 0 && (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-3 font-medium">Domain</th>
                <th className="px-4 py-3 font-medium">Status</th>
                <th className="px-4 py-3 font-medium">Receiving</th>
                <th className="px-4 py-3 font-medium">Sending</th>
              </tr>
            </thead>
            <tbody>
              {data.domains.map((d) => (
                <tr key={d.id} className="border-b border-border last:border-0">
                  <td className="px-4 py-3">
                    <Link to={`/domains/${d.id}`} className="font-medium text-primary hover:underline">
                      {d.name}
                    </Link>
                  </td>
                  <td className="px-4 py-3">
                    <StatusBadge status={d.status} />
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">{d.receiving ? "on" : "off"}</td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {d.sending_paused ? "paused" : "active"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {showAdd && <AddDomainDialog onClose={() => setShowAdd(false)} />}
    </div>
  );
}

function AddDomainDialog({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState("");
  const [receiving, setReceiving] = useState(false);
  const qc = useQueryClient();
  const mut = useMutation({
    mutationFn: () => createDomain(name.trim(), receiving),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domains"] });
      onClose();
    },
  });

  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label="add-domain">
      <Card className="w-full max-w-md p-6">
        <h2 className="mb-4 text-lg font-semibold">Add domain</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            mut.mutate();
          }}
          className="space-y-4"
        >
          <div className="space-y-1">
            <label htmlFor="domain-name" className="text-sm font-medium">
              Domain name
            </label>
            <input
              id="domain-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="example.com"
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
            />
          </div>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={receiving} onChange={(e) => setReceiving(e.target.checked)} />
            Enable inbound receiving (adds an MX record)
          </label>
          {mut.isError && (
            <p role="alert" className="text-sm text-destructive">
              {(mut.error as ApiError).message}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={mut.isPending || !name.trim()}>
              {mut.isPending ? "Creating…" : "Create"}
            </Button>
          </div>
        </form>
      </Card>
    </div>
  );
}
