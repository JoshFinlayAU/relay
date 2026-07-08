import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { setToken, clearToken } from "../lib/auth";
import { login } from "../api/auth";
import { ApiError } from "../lib/api";

export default function Login() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const navigate = useNavigate();

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    if (!username.trim() || !password) {
      setError("Enter your username and password.");
      return;
    }
    setBusy(true);
    try {
      const res = await login(username.trim(), password);
      setToken(res.token);
      navigate("/", { replace: true });
    } catch (err) {
      clearToken();
      setError(err instanceof ApiError ? err.message : "Login failed.");
    } finally {
      setBusy(false);
    }
  }

  const field =
    "w-full rounded-xl bg-background/60 px-3.5 py-2.5 text-sm outline-none ring-1 ring-inset ring-white/10 transition-all duration-300 ease-spring focus:ring-2 focus:ring-primary/70";

  return (
    <div className="flex min-h-[100dvh] items-center justify-center p-6">
      <div className="w-full max-w-sm rounded-4xl bg-white/[0.03] p-2 shadow-lift ring-1 ring-inset ring-white/[0.06] animate-rise">
        <form
          onSubmit={onSubmit}
          className="space-y-5 rounded-[calc(2rem-0.5rem)] bg-card p-8 shadow-inner-hi"
          aria-label="login"
        >
          <div className="space-y-3">
            <div className="flex h-11 w-11 items-center justify-center rounded-2xl bg-primary shadow-soft shadow-inner-hi">
              <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                <path d="M3 5l9 7 9-7M3 5v14h18V5" />
              </svg>
            </div>
            <h1 className="text-2xl font-bold tracking-tight">Relay</h1>
            <p className="text-sm text-muted-foreground">Sign in to the admin console.</p>
          </div>
          <div className="space-y-1.5">
            <label htmlFor="username" className="text-xs font-medium text-muted-foreground">Username</label>
            <input id="username" name="username" autoComplete="username" value={username}
              onChange={(e) => setUsername(e.target.value)} className={field} />
          </div>
          <div className="space-y-1.5">
            <label htmlFor="password" className="text-xs font-medium text-muted-foreground">Password</label>
            <input id="password" name="password" type="password" autoComplete="current-password" value={password}
              onChange={(e) => setPassword(e.target.value)} className={field} />
          </div>
          {error && <p role="alert" className="text-sm text-rose">{error}</p>}
          <button
            type="submit"
            disabled={busy}
            className="w-full rounded-full bg-primary px-4 py-2.5 text-sm font-semibold text-primary-foreground shadow-soft shadow-inner-hi transition-all duration-300 ease-spring hover:brightness-110 active:scale-[0.98] disabled:opacity-50"
          >
            {busy ? "Signing in…" : "Sign in"}
          </button>
        </form>
      </div>
    </div>
  );
}
