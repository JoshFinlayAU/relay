import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createApiKey, listApiKeys, revokeApiKey, type CreateApiKeyResponse } from "../api/apikeys";
import { Button, Card, CopyButton } from "../components/ui";
import { ApiError } from "../lib/api";
import { cn } from "../lib/utils";

export default function ApiKeys() {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["api-keys"], queryFn: listApiKeys });
  const [name, setName] = useState("");
  const [revealed, setRevealed] = useState<CreateApiKeyResponse | null>(null);

  const invalidate = () => qc.invalidateQueries({ queryKey: ["api-keys"] });
  const createMut = useMutation({
    mutationFn: () => createApiKey(name.trim()),
    onSuccess: (r) => { setRevealed(r); setName(""); invalidate(); },
  });
  const revokeMut = useMutation({ mutationFn: (id: string) => revokeApiKey(id), onSuccess: invalidate });

  const keys = data?.api_keys ?? [];

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">API keys</h1>
      <p className="text-sm text-muted-foreground">
        Bearer tokens for programmatic access to the <span className="font-mono text-xs">/v1</span> API
        (domain onboarding, verification, mailboxes, stats). Send as{" "}
        <span className="font-mono text-xs">Authorization: Bearer &lt;token&gt;</span>. Shown once at
        creation — store it securely.
      </p>

      <Card className="p-4">
        <form
          onSubmit={(e) => { e.preventDefault(); createMut.mutate(); }}
          className="flex flex-wrap items-end gap-3"
        >
          <div className="flex-1 space-y-1">
            <label htmlFor="key-name" className="text-sm font-medium">Name</label>
            <input
              id="key-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. provisioning-bot"
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
            />
          </div>
          <Button type="submit" disabled={createMut.isPending || !name.trim()} data-testid="create-api-key">
            {createMut.isPending ? "Creating…" : "Create key"}
          </Button>
        </form>
        {createMut.isError && (
          <p role="alert" className="mt-2 text-sm text-destructive">{(createMut.error as ApiError).message}</p>
        )}
      </Card>

      {keys.length === 0 ? (
        <Card className="p-8 text-center text-sm text-muted-foreground">No API keys yet.</Card>
      ) : (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium">Name</th>
                <th className="px-4 py-2 font-medium">Created</th>
                <th className="px-4 py-2 font-medium">Last used</th>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {keys.map((k) => (
                <tr key={k.id} className="border-b border-border last:border-0" data-testid="api-key-row">
                  <td className="px-4 py-2">{k.name}</td>
                  <td className="px-4 py-2 text-muted-foreground">{k.created_at ? new Date(k.created_at).toLocaleString() : ""}</td>
                  <td className="px-4 py-2 text-muted-foreground">{k.last_used ? new Date(k.last_used).toLocaleString() : "never"}</td>
                  <td className="px-4 py-2">
                    <span className={cn("rounded-full border px-2 py-0.5 text-xs",
                      k.revoked ? "border-red-500/30 bg-red-500/15 text-red-400" : "border-green-500/30 bg-green-500/15 text-green-400")}>
                      {k.revoked ? "revoked" : "active"}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-right">
                    {!k.revoked && (
                      <Button variant="ghost" className="text-xs text-destructive"
                        onClick={() => { if (window.confirm(`Revoke API key "${k.name}"? Any client using it will stop working.`)) revokeMut.mutate(k.id); }}>
                        Revoke
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {revealed && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" role="dialog" aria-label="api-key-secret">
          <Card bezel>
            <div className="w-full max-w-lg p-6">
              <h2 className="mb-2 text-lg font-semibold">API key created</h2>
              <p className="mb-4 rounded-md border border-orange-500/30 bg-orange-500/10 p-2 text-sm text-orange-300">
                Copy it now — it is shown only once and cannot be retrieved again.
              </p>
              <div className="flex items-center gap-2">
                <code data-testid="api-key-secret" className="flex-1 break-all rounded bg-muted px-2 py-1 font-mono text-xs">{revealed.token}</code>
                <CopyButton text={revealed.token} />
              </div>
              <div className="mt-6 flex justify-end">
                <Button onClick={() => setRevealed(null)}>Done</Button>
              </div>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}
