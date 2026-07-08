import { clearToken, getToken } from "./auth";

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

// Raised on 401 so the router can redirect to /login.
export class UnauthorizedError extends ApiError {}

export async function api<T = unknown>(
  path: string,
  opts: RequestInit = {},
): Promise<T> {
  const token = getToken();
  const headers = new Headers(opts.headers);
  headers.set("Accept", "application/json");
  if (opts.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  if (token) headers.set("Authorization", `Bearer ${token}`);

  const res = await fetch(path, { ...opts, headers });

  const text = await res.text();
  // Guard JSON.parse: a non-JSON body (gateway 502/504 HTML, SPA fallback for a
  // mistyped path) must not throw before the status checks below - otherwise a
  // 401 with a non-JSON body would skip the clearToken()/redirect path.
  let data: unknown;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch {
      data = undefined;
    }
  }
  const err = (data as { error?: { code?: string; message?: string } } | undefined)?.error;

  if (res.status === 401) {
    clearToken();
    // Carry the server's real code/message (e.g. invalid_credentials) so the
    // login screen can show it; genuine session-expiry still redirects via the
    // global handler (which skips the /login route).
    throw new UnauthorizedError(
      401,
      err?.code ?? "unauthorized",
      err?.message ?? "Your session has expired.",
    );
  }

  if (!res.ok) {
    throw new ApiError(res.status, err?.code ?? "error", err?.message ?? res.statusText);
  }
  return data as T;
}
