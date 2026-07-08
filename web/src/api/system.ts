import { api } from "../lib/api";

export interface Event {
  type: string;
  domain_id: string | null;
  detail: unknown;
  created_at: string | null;
}

export interface ServerInfo {
  hostname: string;
  version: string;
  tls_enabled: boolean;
  listeners: Record<string, string>;
  queue_depth: number;
  database: { ok: boolean };
  cert: { managed: boolean; not_after?: string; days_remaining?: number };
  sending_ipv4: string;
  sending_ipv6: string;
}

export function listEvents(params: Record<string, string> = {}): Promise<{ events: Event[]; next_cursor: string }> {
  const qs = new URLSearchParams(params).toString();
  return api(`/v1/events${qs ? "?" + qs : ""}`);
}

export function getServerInfo(): Promise<ServerInfo> {
  return api("/v1/server/info");
}

export interface Stats {
  submitted: number;
  delivered: number;
  deferred: number;
  bounced_hard: number;
  bounced_soft: number;
  complaints: number;
}

export interface TimeseriesBucket {
  bucket: string;
  submitted: number;
  delivered: number;
  deferred: number;
  bounced_hard: number;
  bounced_soft: number;
  complaints: number;
}

export function getDomainStats(id: string, window = "24h"): Promise<{ stats: Stats; window: string }> {
  return api(`/v1/domains/${id}/stats?window=${window}`);
}

export function getDomainTimeseries(id: string, window = "7d"): Promise<{ buckets: TimeseriesBucket[] }> {
  return api(`/v1/domains/${id}/stats/timeseries?window=${window}`);
}

export function getCredentialStats(id: string, window = "24h"): Promise<{ stats: Stats; window: string }> {
  return api(`/v1/credentials/${id}/stats?window=${window}`);
}

export function testSend(domainId: string, to: string): Promise<{ message_id: string; trace_url: string }> {
  return api(`/v1/domains/${domainId}/test-send`, { method: "POST", body: JSON.stringify({ to }) });
}
