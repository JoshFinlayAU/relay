import { api } from "../lib/api";

export type DomainStatus = "pending" | "active" | "degraded" | "suspended";

export interface Domain {
  id: string;
  name: string;
  status: DomainStatus;
  receiving: boolean;
  sending_paused: boolean;
  forward_bounces: boolean;
  bounce_subdomain: string;
  created_at: string | null;
  updated_at: string | null;
}

export type CheckResult = "unknown" | "pass" | "fail" | "warn";

export interface DnsInstruction {
  purpose: string;
  type: string;
  name: string;
  value: string;
  zone_line: string;
  required: boolean;
  last_result: CheckResult;
  observed?: string;
  detail?: string;
  last_checked?: string;
  conflict?: boolean;
  merged_value?: string;
}

export interface DnsResponse {
  domain: string;
  status: DomainStatus;
  instructions: DnsInstruction[];
  operator_note: string;
}

export interface VerifyResult {
  purpose: string;
  result: CheckResult;
  observed: string;
  detail: string;
}

export interface VerifyResponse {
  domain: Domain;
  results: VerifyResult[];
  active: boolean;
}

export interface CreateDomainResponse {
  domain: Domain;
  dns: DnsInstruction[];
}

export function listDomains(): Promise<{ domains: Domain[]; next_cursor: string }> {
  return api("/v1/domains");
}

export function getDomain(id: string): Promise<{ domain: Domain }> {
  return api(`/v1/domains/${id}`);
}

export function createDomain(name: string, receiving: boolean): Promise<CreateDomainResponse> {
  return api("/v1/domains", {
    method: "POST",
    body: JSON.stringify({ name, receiving }),
  });
}

export function getDNS(id: string): Promise<DnsResponse> {
  return api(`/v1/domains/${id}/dns`);
}

export function verifyDomain(id: string): Promise<VerifyResponse> {
  return api(`/v1/domains/${id}/verify`, { method: "POST" });
}

export interface ProvisionResult {
  purpose: string;
  name: string;
  type: string;
  action: string;
  error?: string;
}

export function provisionDNS(id: string, apiToken: string): Promise<{ results: ProvisionResult[]; note: string }> {
  return api(`/v1/domains/${id}/dns/provision`, {
    method: "POST",
    body: JSON.stringify({ provider: "cloudflare", api_token: apiToken }),
  });
}

export function patchDomain(
  id: string,
  patch: Partial<Pick<Domain, "receiving" | "sending_paused" | "forward_bounces">>,
): Promise<{ domain: Domain }> {
  return api(`/v1/domains/${id}`, { method: "PATCH", body: JSON.stringify(patch) });
}

export function deleteDomain(id: string): Promise<void> {
  return api(`/v1/domains/${id}`, { method: "DELETE" });
}
