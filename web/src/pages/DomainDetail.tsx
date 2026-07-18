import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { deleteDomain, getDomain, patchDomain } from "../api/domains";
import { Button, Card, PageHeader, StatusBadge, Switch } from "../components/ui";
import Credentials from "../components/Credentials";
import DmarcPanel from "../components/DmarcPanel";
import Mailboxes from "../components/Mailboxes";
import Suppressions from "../components/Suppressions";
import DomainStats from "../components/DomainStats";
import DnsPanel from "../components/DnsPanel";

export default function DomainDetail() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();

  const domainQ = useQuery({ queryKey: ["domain", id], queryFn: () => getDomain(id) });

  const patchMut = useMutation({
    mutationFn: (patch: Parameters<typeof patchDomain>[1]) => patchDomain(id, patch),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domain", id] });
      qc.invalidateQueries({ queryKey: ["dns", id] });
      qc.invalidateQueries({ queryKey: ["domains"] }); // keep the list's status columns fresh
    },
  });

  const delMut = useMutation({
    mutationFn: () => deleteDomain(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["domains"] });
      navigate("/domains", { replace: true });
    },
  });

  const [confirmDelete, setConfirmDelete] = useState(false);
  const domain = domainQ.data?.domain;

  if (domainQ.isLoading) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (domainQ.isError || !domain)
    return <p role="alert" className="text-sm text-destructive">Domain not found.</p>;

  return (
    <div className="space-y-8">
      <PageHeader
        eyebrow="Domain"
        title={domain.name}
        actions={<Button variant="destructive" onClick={() => setConfirmDelete(true)}>Delete</Button>}
      />
      <div className="-mt-4"><StatusBadge status={domain.status} /></div>

      {/* Settings toggles */}
      <Card className="flex flex-wrap gap-8 p-5">
        <Switch label="Inbound receiving" checked={domain.receiving} onChange={(v) => patchMut.mutate({ receiving: v })} />
        <Switch label="Pause sending" checked={domain.sending_paused} onChange={(v) => patchMut.mutate({ sending_paused: v })} />
        <Switch label="Forward bounces to webhook" checked={domain.forward_bounces} onChange={(v) => patchMut.mutate({ forward_bounces: v })} />
      </Card>

      {/* Delivery expiry */}
      <DeliveryExpiry
        seconds={domain.delivery_max_age_seconds}
        saving={patchMut.isPending}
        onSave={(secs) => patchMut.mutate({ delivery_max_age_seconds: secs })}
      />

      {/* DNS - traffic-light, collapsible, auto-configure */}
      <DnsPanel domainId={id} />

      {/* Statistics + test send */}
      <DomainStats domainId={id} />

      {/* DMARC analyzer */}
      <DmarcPanel domainId={id} />

      <Credentials domainId={id} domainName={domain.name} />
      <Mailboxes domainId={id} />
      <Suppressions domainId={id} />

      {confirmDelete && (
        <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" role="dialog" aria-label="confirm-delete">
          <Card bezel>
            <div className="w-full max-w-sm p-6">
              <h2 className="mb-2 text-lg font-semibold">Delete {domain.name}?</h2>
              <p className="mb-5 text-sm text-muted-foreground">
                This removes the domain, its DKIM key, and DNS records. This cannot be undone.
              </p>
              <div className="flex justify-end gap-2">
                <Button variant="secondary" onClick={() => setConfirmDelete(false)}>Cancel</Button>
                <Button variant="destructive" onClick={() => delMut.mutate()} disabled={delMut.isPending}>Delete</Button>
              </div>
            </div>
          </Card>
        </div>
      )}
    </div>
  );
}

function DeliveryExpiry({
  seconds,
  saving,
  onSave,
}: {
  seconds: number | null;
  saving: boolean;
  onSave: (secs: number) => void;
}) {
  const [hours, setHours] = useState(seconds ? String(seconds / 3600) : "");
  const usingDefault = seconds == null;
  const save = () => {
    const h = parseFloat(hours);
    onSave(!hours.trim() || isNaN(h) || h <= 0 ? 0 : Math.round(h * 3600));
  };
  return (
    <Card className="space-y-3 p-5 text-sm">
      <div>
        <h2 className="text-base font-semibold">Delivery expiry</h2>
        <p className="text-muted-foreground">
          How long a deferred message keeps retrying before it&apos;s failed/bounced.{" "}
          {usingDefault
            ? "Currently using the server default."
            : `Currently ${seconds! / 3600}h for this domain.`}
        </p>
      </div>
      <div className="flex items-center gap-2">
        <input
          type="number"
          min={0}
          step="0.5"
          aria-label="Delivery expiry hours"
          value={hours}
          onChange={(e) => setHours(e.target.value)}
          placeholder="default"
          className="w-28 rounded-md border border-border bg-background px-3 py-1.5 outline-none focus:border-primary"
        />
        <span className="text-muted-foreground">hours (blank / 0 = server default)</span>
        <Button className="ml-2" onClick={save} disabled={saving} data-testid="save-expiry">
          {saving ? "Saving…" : "Save"}
        </Button>
      </div>
    </Card>
  );
}
