// Bearer token stored in memory + sessionStorage (never localStorage), per spec.
const KEY = "relay_token";

let inMemory: string | null = null;

export function getToken(): string | null {
  if (inMemory) return inMemory;
  const t = sessionStorage.getItem(KEY);
  inMemory = t;
  return t;
}

export function setToken(token: string) {
  inMemory = token;
  sessionStorage.setItem(KEY, token);
}

export function clearToken() {
  inMemory = null;
  sessionStorage.removeItem(KEY);
}

export function isAuthed(): boolean {
  return !!getToken();
}
