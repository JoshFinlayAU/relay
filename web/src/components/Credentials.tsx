import { Fragment, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createCredential,
  deleteCredential,
  listCredentials,
  patchCredential,
  type CreateCredentialResponse,
  type Restrictions,
} from "../api/credentials";
import { Button, Card, CopyButton } from "./ui";
import { ApiError } from "../lib/api";
import { cn } from "../lib/utils";
import CredentialStats from "./CredentialStats";

export default function Credentials({ domainId, domainName }: { domainId: string; domainName: string }) {
  const [showCreate, setShowCreate] = useState(false);
  const [revealed, setRevealed] = useState<CreateCredentialResponse | null>(null);
  const [statsFor, setStatsFor] = useState<string | null>(null);
  const qc = useQueryClient();

  const { data } = useQuery({
    queryKey: ["credentials", domainId],
    queryFn: () => listCredentials(domainId),
  });

  const patchMut = useMutation({
    mutationFn: (v: { id: string; status: "active" | "suspended" | "revoked" }) =>
      patchCredential(v.id, { status: v.status }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["credentials", domainId] }),
  });
  const delMut = useMutation({
    mutationFn: (id: string) => deleteCredential(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["credentials", domainId] }),
  });

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">SMTP credentials</h2>
        <Button onClick={() => setShowCreate(true)} data-testid="add-credential">
          Add credential
        </Button>
      </div>

      {data && data.credentials.length === 0 && (
        <Card className="p-6 text-center text-sm text-muted-foreground">
          No credentials yet. Create one to get SMTP AUTH details for an app.
        </Card>
      )}

      {data && data.credentials.length > 0 && (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium">Username</th>
                <th className="px-4 py-2 font-medium">Status</th>
                <th className="px-4 py-2 font-medium">Last used</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {data.credentials.map((c) => (
                <Fragment key={c.id}>
                <tr className="border-b border-border last:border-0" data-testid="credential-row">
                  <td className="px-4 py-2 font-mono text-xs">{c.username}</td>
                  <td className="px-4 py-2">
                    <span
                      className={cn(
                        "rounded-full border px-2 py-0.5 text-xs capitalize",
                        c.status === "active"
                          ? "border-green-500/30 bg-green-500/15 text-green-400"
                          : "border-orange-500/30 bg-orange-500/15 text-orange-400",
                      )}
                    >
                      {c.status}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-muted-foreground">
                    {c.last_used ? new Date(c.last_used).toLocaleString() : "never"}
                  </td>
                  <td className="px-4 py-2 text-right">
                    <Button
                      variant="ghost"
                      className="text-xs"
                      data-testid="credential-stats-toggle"
                      onClick={() => setStatsFor((v) => (v === c.id ? null : c.id))}
                    >
                      {statsFor === c.id ? "Hide stats" : "Stats"}
                    </Button>
                    {c.status === "active" ? (
                      <Button variant="ghost" className="text-xs" onClick={() => patchMut.mutate({ id: c.id, status: "suspended" })}>
                        Suspend
                      </Button>
                    ) : (
                      <Button variant="ghost" className="text-xs" onClick={() => patchMut.mutate({ id: c.id, status: "active" })}>
                        Resume
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      className="text-xs text-destructive"
                      onClick={() => {
                        if (window.confirm(`Delete credential "${c.username}"? Its SMTP secret is unrecoverable and any app using it will stop sending.`)) {
                          delMut.mutate(c.id);
                        }
                      }}
                    >
                      Delete
                    </Button>
                  </td>
                </tr>
                {statsFor === c.id && (
                  <tr>
                    <td colSpan={4} className="bg-white/[0.01] p-0">
                      <CredentialStats credentialId={c.id} />
                    </td>
                  </tr>
                )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {showCreate && (
        <CreateCredentialDialog
          domainId={domainId}
          domainName={domainName}
          onClose={() => setShowCreate(false)}
          onCreated={(resp) => {
            setShowCreate(false);
            setRevealed(resp);
            qc.invalidateQueries({ queryKey: ["credentials", domainId] });
          }}
        />
      )}

      {revealed && <SecretReveal resp={revealed} onClose={() => setRevealed(null)} />}
    </div>
  );
}

function CreateCredentialDialog({
  domainId,
  domainName,
  onClose,
  onCreated,
}: {
  domainId: string;
  domainName: string;
  onClose: () => void;
  onCreated: (r: CreateCredentialResponse) => void;
}) {
  const [name, setName] = useState("");
  const [maxRcpt, setMaxRcpt] = useState("");
  const [maxPerHour, setMaxPerHour] = useState("");

  const mut = useMutation({
    mutationFn: () => {
      const restrictions: Restrictions = {};
      if (maxRcpt) restrictions.max_recipients = Number(maxRcpt);
      if (maxPerHour) restrictions.max_messages_per_hour = Number(maxPerHour);
      return createCredential(domainId, name.trim(), restrictions);
    },
    onSuccess: onCreated,
  });

  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label="add-credential">
      <Card className="w-full max-w-md p-6">
        <h2 className="mb-4 text-lg font-semibold">New credential</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            mut.mutate();
          }}
          className="space-y-4"
        >
          <div className="space-y-1">
            <label htmlFor="cred-name" className="text-sm font-medium">
              App name
            </label>
            <div className="flex items-center gap-1 text-sm">
              <input
                id="cred-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="orders"
                className="w-full rounded-md border border-border bg-background px-3 py-2 outline-none focus:border-primary"
              />
              <span className="whitespace-nowrap text-muted-foreground">@{domainName}</span>
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label htmlFor="cred-rcpt" className="text-xs font-medium">
                Max recipients/msg
              </label>
              <input
                id="cred-rcpt"
                type="number"
                value={maxRcpt}
                onChange={(e) => setMaxRcpt(e.target.value)}
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="cred-rate" className="text-xs font-medium">
                Max msgs/hour
              </label>
              <input
                id="cred-rate"
                type="number"
                value={maxPerHour}
                onChange={(e) => setMaxPerHour(e.target.value)}
                className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary"
              />
            </div>
          </div>
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

function SecretReveal({ resp, onClose }: { resp: CreateCredentialResponse; onClose: () => void }) {
  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label="secret-reveal">
      <Card className="w-full max-w-lg p-6">
        <h2 className="mb-2 text-lg font-semibold">Credential created</h2>
        <p className="mb-4 rounded-md border border-orange-500/30 bg-orange-500/10 p-2 text-sm text-orange-300">
          Copy the secret now - it is shown only once and cannot be retrieved again.
        </p>
        <div className="space-y-2 text-sm">
          <Field label="SMTP username" value={resp.credential.username} />
          <Field label="SMTP password" value={resp.secret} testid="secret-value" mono />
        </div>
        <div className="mt-6 flex justify-end">
          <Button onClick={onClose}>Done</Button>
        </div>
      </Card>
    </div>
  );
}

function Field({ label, value, mono, testid }: { label: string; value: string; mono?: boolean; testid?: string }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="flex items-center gap-2">
        <code
          data-testid={testid}
          className={cn("flex-1 break-all rounded bg-muted px-2 py-1", mono && "font-mono text-xs")}
        >
          {value}
        </code>
        <CopyButton text={value} />
      </div>
    </div>
  );
}
