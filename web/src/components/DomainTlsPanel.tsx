import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { deleteDomainTLS, getDomainTLS, putDomainTLS } from "../api/tls";
import { Button, Card } from "./ui";
import { ApiError } from "../lib/api";

export default function DomainTlsPanel({ domainId }: { domainId: string }) {
  const qc = useQueryClient();
  const { data } = useQuery({ queryKey: ["domain-tls", domainId], queryFn: () => getDomainTLS(domainId) });
  const [open, setOpen] = useState(false);
  const [cert, setCert] = useState("");
  const [key, setKey] = useState("");

  const invalidate = () => qc.invalidateQueries({ queryKey: ["domain-tls", domainId] });
  const save = useMutation({
    mutationFn: () => putDomainTLS(domainId, cert.trim(), key.trim()),
    onSuccess: () => { setOpen(false); setCert(""); setKey(""); invalidate(); },
  });
  const remove = useMutation({ mutationFn: () => deleteDomainTLS(domainId), onSuccess: invalidate });

  const configured = data?.configured;

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">TLS certificate</h2>
        {!open && (
          <Button variant="secondary" onClick={() => setOpen(true)} data-testid="add-domain-cert">
            {configured ? "Replace certificate" : "Add certificate"}
          </Button>
        )}
      </div>

      {configured ? (
        <Card className="space-y-2 p-4 text-sm">
          <div className="flex items-center justify-between">
            <span className="rounded-full border border-green-500/30 bg-green-500/15 px-2 py-0.5 text-xs text-green-400">
              manual certificate
            </span>
            <Button
              variant="ghost"
              className="text-xs text-destructive"
              onClick={() => { if (window.confirm("Remove this domain's certificate? It will fall back to the server certificate.")) remove.mutate(); }}
            >
              Remove
            </Button>
          </div>
          {data?.subjects && (
            <div className="font-mono text-xs text-muted-foreground break-all">
              {data.subjects.join(", ")}
            </div>
          )}
          {data?.not_after && (
            <div className="text-xs text-muted-foreground">Expires {new Date(data.not_after).toLocaleDateString()}</div>
          )}
        </Card>
      ) : (
        <Card className="p-4 text-sm text-muted-foreground">
          No per-domain certificate — TLS handshakes for this domain use the server certificate.
          Add one to serve a matching cert by SNI (e.g. clients connecting to a host under this domain).
        </Card>
      )}

      {open && (
        <Card className="space-y-3 p-4 text-sm">
          <div className="space-y-1">
            <label htmlFor="cert-pem" className="text-xs font-medium">Certificate (PEM, full chain — leaf first)</label>
            <textarea id="cert-pem" value={cert} onChange={(e) => setCert(e.target.value)} rows={5}
              placeholder="-----BEGIN CERTIFICATE-----"
              className="w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary" />
          </div>
          <div className="space-y-1">
            <label htmlFor="key-pem" className="text-xs font-medium">Private key (PEM)</label>
            <textarea id="key-pem" value={key} onChange={(e) => setKey(e.target.value)} rows={4}
              placeholder="-----BEGIN PRIVATE KEY-----"
              className="w-full rounded-md border border-border bg-background px-3 py-2 font-mono text-xs outline-none focus:border-primary" />
          </div>
          {save.isError && <p role="alert" className="text-sm text-destructive">{(save.error as ApiError).message}</p>}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={() => setOpen(false)}>Cancel</Button>
            <Button onClick={() => save.mutate()} disabled={save.isPending || !cert.trim() || !key.trim()} data-testid="save-domain-cert">
              {save.isPending ? "Saving…" : "Save certificate"}
            </Button>
          </div>
        </Card>
      )}
    </div>
  );
}
