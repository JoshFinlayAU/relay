import { useQuery } from "@tanstack/react-query";
import { getServerInfo } from "../api/system";
import { Card } from "../components/ui";
import { cn } from "../lib/utils";

export default function Settings() {
  const { data, isLoading } = useQuery({ queryKey: ["server-info"], queryFn: getServerInfo, refetchInterval: 15000 });

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Settings</h1>
      {isLoading && <p className="text-sm text-muted-foreground">Loading…</p>}
      {data && (
        <>
          <Card className="grid grid-cols-2 gap-x-8 gap-y-3 p-5 text-sm md:grid-cols-3">
            <Field label="Hostname" value={data.hostname} mono />
            <Field label="Version" value={data.version} />
            <Field label="Database" value={data.database.ok ? "connected" : "down"} accent={data.database.ok ? "text-green-400" : "text-red-400"} />
            <Field label="Queue depth" value={String(data.queue_depth)} />
            <Field label="Sending IPv4" value={data.sending_ipv4} mono />
            <Field label="Sending IPv6" value={data.sending_ipv6} mono />
          </Card>

          <div>
            <h2 className="mb-2 text-lg font-semibold">TLS</h2>
            <Card className="grid grid-cols-2 gap-x-8 gap-y-3 p-5 text-sm md:grid-cols-3">
              <Field label="TLS" value={data.tls_enabled ? "enabled (Let's Encrypt)" : "disabled (dev)"} accent={data.tls_enabled ? "text-green-400" : "text-orange-400"} />
              {data.cert.not_after && <Field label="Cert expires" value={new Date(data.cert.not_after).toLocaleDateString()} />}
              {typeof data.cert.days_remaining === "number" && (
                <Field label="Days remaining" value={String(data.cert.days_remaining)} accent={data.cert.days_remaining < 14 ? "text-orange-400" : undefined} />
              )}
            </Card>
          </div>

          <div>
            <h2 className="mb-2 text-lg font-semibold">Listeners</h2>
            <Card className="p-5">
              <table className="w-full text-sm">
                <tbody>
                  {Object.entries(data.listeners).map(([name, addr]) => (
                    <tr key={name} className="border-b border-border last:border-0">
                      <td className="px-2 py-2 capitalize text-muted-foreground">{name}</td>
                      <td className="px-2 py-2 font-mono">{addr}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </Card>
          </div>

          <p className="text-xs text-muted-foreground">
            Admin accounts are managed under <span className="font-medium">Admin users</span>. API
            automation uses static bearer tokens configured on the server.
          </p>
        </>
      )}
    </div>
  );
}

function Field({ label, value, mono, accent }: { label: string; value: string; mono?: boolean; accent?: string }) {
  return (
    <div>
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={cn("mt-0.5", mono && "font-mono text-xs", accent)}>{value}</div>
    </div>
  );
}
