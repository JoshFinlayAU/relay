import { api } from "../lib/api";

export interface Suppression {
  address: string;
  reason: string | null;
  created_at: string | null;
}

export function listSuppressions(domainId: string): Promise<{ suppressions: Suppression[] }> {
  return api(`/v1/domains/${domainId}/suppressions`);
}

export function addSuppression(domainId: string, address: string, reason?: string): Promise<unknown> {
  return api(`/v1/domains/${domainId}/suppressions`, {
    method: "POST",
    body: JSON.stringify({ address, reason }),
  });
}

export function removeSuppression(domainId: string, address: string): Promise<void> {
  return api(`/v1/domains/${domainId}/suppressions?address=${encodeURIComponent(address)}`, {
    method: "DELETE",
  });
}
