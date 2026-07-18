import { api } from "../lib/api";

export interface ServerTLS {
  source: string; // acme | manual-file | self-signed | disabled
  not_after?: string;
  days_remaining?: number;
}

export interface DomainTLS {
  configured: boolean;
  subjects?: string[];
  not_before?: string | null;
  not_after?: string | null;
  updated_at?: string | null;
}

export function getServerTLS(): Promise<ServerTLS> {
  return api("/v1/settings/tls");
}

export function reloadServerTLS(): Promise<ServerTLS> {
  return api("/v1/settings/tls/reload", { method: "POST" });
}

export function getDomainTLS(id: string): Promise<DomainTLS> {
  return api(`/v1/domains/${id}/tls-cert`);
}

export function putDomainTLS(id: string, certPem: string, keyPem: string): Promise<DomainTLS> {
  return api(`/v1/domains/${id}/tls-cert`, {
    method: "PUT",
    body: JSON.stringify({ cert_pem: certPem, key_pem: keyPem }),
  });
}

export function deleteDomainTLS(id: string): Promise<void> {
  return api(`/v1/domains/${id}/tls-cert`, { method: "DELETE" });
}
