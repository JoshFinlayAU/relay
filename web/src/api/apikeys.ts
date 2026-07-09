import { api } from "../lib/api";

export interface ApiKey {
  id: string;
  name: string;
  created_at: string | null;
  last_used: string | null;
  revoked: boolean;
}

export interface CreateApiKeyResponse {
  api_key: ApiKey;
  token: string; // shown once
}

export function listApiKeys(): Promise<{ api_keys: ApiKey[] }> {
  return api("/v1/api-keys");
}

export function createApiKey(name: string): Promise<CreateApiKeyResponse> {
  return api("/v1/api-keys", { method: "POST", body: JSON.stringify({ name }) });
}

export function revokeApiKey(id: string): Promise<void> {
  return api(`/v1/api-keys/${id}`, { method: "DELETE" });
}
