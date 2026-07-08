import { api } from "../lib/api";

export interface Restrictions {
  allowed_from?: string[];
  max_messages_per_hour?: number;
  max_recipients?: number;
  max_message_size?: number;
}

export type CredentialStatus = "active" | "suspended" | "revoked";

export interface Credential {
  id: string;
  domain_id: string;
  username: string;
  status: CredentialStatus;
  restrictions: Restrictions;
  last_used: string | null;
  created_at: string | null;
  updated_at: string | null;
}

export interface CreateCredentialResponse {
  credential: Credential;
  secret: string;
}

export function listCredentials(domainId: string): Promise<{ credentials: Credential[] }> {
  return api(`/v1/domains/${domainId}/credentials`);
}

export function createCredential(
  domainId: string,
  name: string,
  restrictions?: Restrictions,
): Promise<CreateCredentialResponse> {
  return api(`/v1/domains/${domainId}/credentials`, {
    method: "POST",
    body: JSON.stringify({ name, restrictions }),
  });
}

export function patchCredential(
  id: string,
  patch: { status?: CredentialStatus; restrictions?: Restrictions },
): Promise<{ credential: Credential }> {
  return api(`/v1/credentials/${id}`, { method: "PATCH", body: JSON.stringify(patch) });
}

export function deleteCredential(id: string): Promise<void> {
  return api(`/v1/credentials/${id}`, { method: "DELETE" });
}
