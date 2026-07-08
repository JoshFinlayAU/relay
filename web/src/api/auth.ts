import { api } from "../lib/api";

export interface AdminUser {
  id: string;
  username: string;
  disabled: boolean;
  created_at: string | null;
  last_login: string | null;
}

export function login(username: string, password: string): Promise<{
  token: string;
  expires_at: string;
  user: { id: string; username: string };
}> {
  return api("/v1/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
}

export function logout(): Promise<void> {
  return api("/v1/auth/logout", { method: "POST" });
}

export function listAdminUsers(): Promise<{ users: AdminUser[] }> {
  return api("/v1/admin/users");
}

export function createAdminUser(username: string, password: string): Promise<{ user: AdminUser }> {
  return api("/v1/admin/users", { method: "POST", body: JSON.stringify({ username, password }) });
}

export function changeAdminPassword(id: string, password: string): Promise<void> {
  return api(`/v1/admin/users/${id}/password`, { method: "POST", body: JSON.stringify({ password }) });
}

export function deleteAdminUser(id: string): Promise<void> {
  return api(`/v1/admin/users/${id}`, { method: "DELETE" });
}
