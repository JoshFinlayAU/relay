import { api } from "../lib/api";

export type RetentionMode = "age" | "count";

export interface RetentionPolicy {
  enabled: boolean;
  mode: RetentionMode;
  days: number;
  max_messages: number;
}

export function getRetention(): Promise<{ policy: RetentionPolicy; source: "custom" | "default" }> {
  return api("/v1/settings/retention");
}

export function setRetention(p: RetentionPolicy): Promise<{ policy: RetentionPolicy; source: string }> {
  return api("/v1/settings/retention", { method: "PUT", body: JSON.stringify(p) });
}
