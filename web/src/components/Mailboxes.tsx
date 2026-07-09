import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  createMailbox,
  deleteMailbox,
  listMailboxes,
  listWebhookDeliveries,
  patchMailbox,
  redeliverWebhook,
  type Mailbox,
} from "../api/mailboxes";
import { Button, Card, CopyButton } from "./ui";
import { ApiError } from "../lib/api";
import { cn } from "../lib/utils";

export default function Mailboxes({ domainId }: { domainId: string }) {
  const [showAdd, setShowAdd] = useState(false);
  const [editing, setEditing] = useState<Mailbox | null>(null);
  const [revealed, setRevealed] = useState<{ mailbox: Mailbox; secret: string } | null>(null);
  const qc = useQueryClient();

  const mailboxesQ = useQuery({ queryKey: ["mailboxes", domainId], queryFn: () => listMailboxes(domainId) });
  const deliveriesQ = useQuery({
    queryKey: ["webhook-deliveries", domainId],
    queryFn: () => listWebhookDeliveries(domainId),
    refetchInterval: 8000,
  });

  const delMut = useMutation({
    mutationFn: (id: string) => deleteMailbox(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mailboxes", domainId] }),
  });
  const redeliverMut = useMutation({
    mutationFn: (id: string) => redeliverWebhook(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["webhook-deliveries", domainId] }),
  });

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Mailboxes &amp; webhooks</h2>
        <Button onClick={() => setShowAdd(true)} data-testid="add-mailbox">Add mailbox</Button>
      </div>
      <p className="text-sm text-muted-foreground">
        Inbound mail to a mailbox is parsed and POSTed to its webhook (HMAC-signed). Use
        <span className="font-mono"> * </span> for a catch-all. Receiving must be enabled on the domain.
      </p>

      {mailboxesQ.data && mailboxesQ.data.mailboxes.length === 0 && (
        <Card className="p-6 text-center text-sm text-muted-foreground">No mailboxes.</Card>
      )}
      {mailboxesQ.data && mailboxesQ.data.mailboxes.length > 0 && (
        <Card>
          <table className="w-full text-sm">
            <thead className="border-b border-border text-left text-muted-foreground">
              <tr>
                <th className="px-4 py-2 font-medium">Local part</th>
                <th className="px-4 py-2 font-medium">Webhook URL</th>
                <th className="px-4 py-2" />
              </tr>
            </thead>
            <tbody>
              {mailboxesQ.data.mailboxes.map((m) => (
                <tr key={m.id} className="border-b border-border last:border-0" data-testid="mailbox-row">
                  <td className="px-4 py-2 font-mono text-xs">{m.local_part}</td>
                  <td className="px-4 py-2 font-mono text-xs">{m.webhook_url}</td>
                  <td className="px-4 py-2 text-right">
                    <Button variant="ghost" className="text-xs" data-testid="edit-webhook" onClick={() => setEditing(m)}>
                      Edit webhook
                    </Button>
                    <Button
                      variant="ghost"
                      className="text-xs text-destructive"
                      onClick={() => {
                        if (window.confirm(`Delete mailbox "${m.local_part}"? Inbound mail to it will no longer be routed to the webhook.`)) {
                          delMut.mutate(m.id);
                        }
                      }}
                    >
                      Delete
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {/* Delivery log */}
      {deliveriesQ.data && deliveriesQ.data.deliveries.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-sm font-semibold text-muted-foreground">Recent webhook deliveries</h3>
          <Card>
            <table className="w-full text-sm">
              <thead className="border-b border-border text-left text-muted-foreground">
                <tr>
                  <th className="px-4 py-2 font-medium">Message</th>
                  <th className="px-4 py-2 font-medium">Attempt</th>
                  <th className="px-4 py-2 font-medium">Result</th>
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody>
                {deliveriesQ.data.deliveries.map((wd) => (
                  <tr key={wd.id} className="border-b border-border last:border-0">
                    <td className="px-4 py-2 font-mono text-xs">{wd.message_id.slice(0, 8)}</td>
                    <td className="px-4 py-2 text-muted-foreground">
                      {wd.attempt_no}{wd.status_code ? ` · ${wd.status_code}` : ""}
                    </td>
                    <td className={cn("px-4 py-2 font-medium",
                      wd.result === "success" ? "text-green-400" : wd.result === "dead_letter" ? "text-red-400" : "text-orange-400")}>
                      {wd.result}
                    </td>
                    <td className="px-4 py-2 text-right">
                      {(wd.result === "dead_letter" || wd.result === "failed") && (
                        <Button variant="ghost" className="text-xs" onClick={() => redeliverMut.mutate(wd.id)}>
                          Re-deliver
                        </Button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Card>
        </div>
      )}

      {showAdd && (
        <AddMailboxDialog
          domainId={domainId}
          onClose={() => setShowAdd(false)}
          onCreated={(r) => {
            setShowAdd(false);
            setRevealed(r);
            qc.invalidateQueries({ queryKey: ["mailboxes", domainId] });
          }}
        />
      )}
      {editing && (
        <EditWebhookDialog
          mailbox={editing}
          onClose={() => setEditing(null)}
          onSaved={(r) => {
            setEditing(null);
            if (r.secret) setRevealed({ mailbox: r.mailbox, secret: r.secret });
            qc.invalidateQueries({ queryKey: ["mailboxes", domainId] });
          }}
        />
      )}
      {revealed && (
        <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label="mailbox-secret">
          <Card className="w-full max-w-lg p-6">
            <h2 className="mb-2 text-lg font-semibold">Mailbox created</h2>
            <p className="mb-4 rounded-md border border-orange-500/30 bg-orange-500/10 p-2 text-sm text-orange-300">
              Copy the webhook signing secret now - it is shown only once. Verify the
              <span className="font-mono"> X-Relay-Signature </span> header with it.
            </p>
            <div className="text-xs text-muted-foreground">Signing secret</div>
            <div className="flex items-center gap-2">
              <code data-testid="mailbox-secret" className="flex-1 break-all rounded bg-muted px-2 py-1 font-mono text-xs">{revealed.secret}</code>
              <CopyButton text={revealed.secret} />
            </div>
            <div className="mt-6 flex justify-end">
              <Button onClick={() => setRevealed(null)}>Done</Button>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}

function EditWebhookDialog({
  mailbox,
  onClose,
  onSaved,
}: {
  mailbox: Mailbox;
  onClose: () => void;
  onSaved: (r: { mailbox: Mailbox; secret?: string }) => void;
}) {
  const [webhookURL, setWebhookURL] = useState(mailbox.webhook_url);
  const [secret, setSecret] = useState("");
  const mut = useMutation({
    mutationFn: () => patchMailbox(mailbox.id, webhookURL.trim(), secret.trim() || undefined),
    onSuccess: onSaved,
  });
  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label="edit-webhook">
      <Card className="w-full max-w-md p-6">
        <h2 className="mb-1 text-lg font-semibold">Edit webhook</h2>
        <p className="mb-4 text-sm text-muted-foreground">Mailbox <span className="font-mono">{mailbox.local_part}</span></p>
        <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }} className="space-y-4">
          <div className="space-y-1">
            <label htmlFor="edit-url" className="text-sm font-medium">Webhook URL</label>
            <input id="edit-url" value={webhookURL} onChange={(e) => setWebhookURL(e.target.value)}
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary" />
          </div>
          <div className="space-y-1">
            <label htmlFor="edit-secret" className="text-sm font-medium">New signing secret <span className="text-muted-foreground">(optional — leave blank to keep)</span></label>
            <input id="edit-secret" value={secret} onChange={(e) => setSecret(e.target.value)} placeholder="rotate signing secret"
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary" />
          </div>
          {mut.isError && <p role="alert" className="text-sm text-destructive">{(mut.error as ApiError).message}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={mut.isPending || !webhookURL.trim()} data-testid="save-webhook">
              {mut.isPending ? "Saving…" : "Save"}
            </Button>
          </div>
        </form>
      </Card>
    </div>
  );
}

function AddMailboxDialog({
  domainId,
  onClose,
  onCreated,
}: {
  domainId: string;
  onClose: () => void;
  onCreated: (r: { mailbox: Mailbox; secret: string }) => void;
}) {
  const [localPart, setLocalPart] = useState("");
  const [webhookURL, setWebhookURL] = useState("");
  const mut = useMutation({
    mutationFn: () => createMailbox(domainId, localPart.trim(), webhookURL.trim()),
    onSuccess: onCreated,
  });
  return (
    <div className="fixed inset-0 flex items-center justify-center bg-black/50 p-4" role="dialog" aria-label="add-mailbox">
      <Card className="w-full max-w-md p-6">
        <h2 className="mb-4 text-lg font-semibold">Add mailbox</h2>
        <form onSubmit={(e) => { e.preventDefault(); mut.mutate(); }} className="space-y-4">
          <div className="space-y-1">
            <label htmlFor="mb-local" className="text-sm font-medium">Local part (or * for catch-all)</label>
            <input id="mb-local" value={localPart} onChange={(e) => setLocalPart(e.target.value)} placeholder="support"
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary" />
          </div>
          <div className="space-y-1">
            <label htmlFor="mb-url" className="text-sm font-medium">Webhook URL</label>
            <input id="mb-url" value={webhookURL} onChange={(e) => setWebhookURL(e.target.value)} placeholder="https://app.example.com/inbound"
              className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm outline-none focus:border-primary" />
          </div>
          {mut.isError && <p role="alert" className="text-sm text-destructive">{(mut.error as ApiError).message}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={mut.isPending || !localPart.trim() || !webhookURL.trim()}>
              {mut.isPending ? "Creating…" : "Create"}
            </Button>
          </div>
        </form>
      </Card>
    </div>
  );
}
