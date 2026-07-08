import { api } from "../lib/api";

export interface Mailbox {
  id: string;
  domain_id: string;
  local_part: string;
  webhook_url: string;
  status: string;
  created_at: string | null;
}

export interface WebhookDelivery {
  id: string;
  message_id: string;
  attempt_no: number;
  status_code: number | null;
  result: string;
  response_snippet: string | null;
  created_at: string | null;
}

export function listMailboxes(domainId: string): Promise<{ mailboxes: Mailbox[] }> {
  return api(`/v1/domains/${domainId}/mailboxes`);
}

export function createMailbox(
  domainId: string,
  localPart: string,
  webhookURL: string,
  secret?: string,
): Promise<{ mailbox: Mailbox; secret: string }> {
  return api(`/v1/domains/${domainId}/mailboxes`, {
    method: "POST",
    body: JSON.stringify({ local_part: localPart, webhook_url: webhookURL, secret }),
  });
}

export function deleteMailbox(id: string): Promise<void> {
  return api(`/v1/mailboxes/${id}`, { method: "DELETE" });
}

export function listWebhookDeliveries(domainId: string): Promise<{ deliveries: WebhookDelivery[] }> {
  return api(`/v1/domains/${domainId}/webhook-deliveries`);
}

export function redeliverWebhook(id: string): Promise<void> {
  return api(`/v1/webhook-deliveries/${id}/redeliver`, { method: "POST" });
}
