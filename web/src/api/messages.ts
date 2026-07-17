import { api } from "../lib/api";
import { getToken } from "../lib/auth";
import type { WebhookDelivery } from "./mailboxes";

export interface Message {
  id: string;
  direction: string;
  status: string;
  mail_from: string | null;
  header_from: string | null;
  rcpt_to: string[];
  subject: string | null;
  size_bytes: number;
  dkim_selector: string | null;
  domain_id: string | null;
  credential_id: string | null;
  spf_result: string | null;
  dkim_result: string | null;
  created_at: string | null;
}

export interface DeliveryAttempt {
  rcpt: string;
  mx_host: string | null;
  result: string;
  smtp_code: number | null;
  smtp_response: string | null;
  tls_version: string | null;
  tls_verified: boolean | null;
  started_at: string | null;
  finished_at: string | null;
}

export interface StatsOverview {
  window: string;
  queue_depth: number;
  degraded_domains: number;
  by_status: Record<string, number>;
  recent_events: { type: string; detail: unknown; created_at: string | null }[];
}

export function listMessages(params: Record<string, string> = {}): Promise<{
  messages: Message[];
  next_cursor: string;
}> {
  const qs = new URLSearchParams(params).toString();
  return api(`/v1/messages${qs ? "?" + qs : ""}`);
}

export interface BounceEvent {
  rcpt: string | null;
  type: string;
  dsn_code: string | null;
  created_at: string | null;
}

export function getMessage(id: string): Promise<{
  message: Message;
  attempts: DeliveryAttempt[];
  bounces: BounceEvent[];
  webhook_deliveries: WebhookDelivery[];
}> {
  return api(`/v1/messages/${id}`);
}

export function getStatsOverview(): Promise<StatsOverview> {
  return api("/v1/stats/overview");
}

// Raw headers are served as text/plain (not JSON), so fetch directly.
export async function getMessageRaw(id: string): Promise<string> {
  const token = getToken();
  const res = await fetch(`/v1/messages/${id}/raw`, {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  });
  if (!res.ok) {
    throw new Error(res.status === 404 ? "Raw message no longer available (retention)." : `Error ${res.status}`);
  }
  return res.text();
}
