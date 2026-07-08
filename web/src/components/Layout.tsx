import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import { ReactNode } from "react";
import { clearToken } from "../lib/auth";
import { logout } from "../api/auth";
import { cn } from "../lib/utils";

// Ultra-light line icons (1.4 stroke), Phosphor/Remix-line style.
const I = ({ d }: { d: string }) => (
  <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
    <path d={d} />
  </svg>
);
const icons: Record<string, ReactNode> = {
  dash: <I d="M3 13h8V3H3v10Zm10 8h8V11h-8v10ZM3 21h8v-6H3v6ZM13 3v6h8V3h-8Z" />,
  domains: <I d="M12 3a9 9 0 100 18 9 9 0 000-18Zm0 0c2.5 2.5 3.5 6 3.5 9s-1 6.5-3.5 9m0-18C9.5 5.5 8.5 9 8.5 12s1 6.5 3.5 9M3.5 9h17M3.5 15h17" />,
  messages: <I d="M4 5h16v12H7l-3 3V5Z" />,
  events: <I d="M12 8v4l3 2M21 12a9 9 0 11-18 0 9 9 0 0118 0Z" />,
  users: <I d="M16 18v-1a4 4 0 00-4-4H7a4 4 0 00-4 4v1M9.5 9a3.5 3.5 0 100-7 3.5 3.5 0 000 7ZM21 18v-1a4 4 0 00-3-3.87M16 3.13A4 4 0 0116 11" />,
  settings: <I d="M12 15a3 3 0 100-6 3 3 0 000 6Z M19.4 15a1.6 1.6 0 00.3 1.8l.1.1a2 2 0 11-2.8 2.8l-.1-.1a1.6 1.6 0 00-2.7.6 1.6 1.6 0 00-1 1.5V22a2 2 0 11-4 0v-.1a1.6 1.6 0 00-1-1.5 1.6 1.6 0 00-1.8.3l-.1.1a2 2 0 11-2.8-2.8l.1-.1a1.6 1.6 0 00-.6-2.7 1.6 1.6 0 00-1.5-1H2a2 2 0 110-4h.1a1.6 1.6 0 001.5-1 1.6 1.6 0 00-.3-1.8l-.1-.1a2 2 0 112.8-2.8l.1.1a1.6 1.6 0 001.8.3H8a1.6 1.6 0 001-1.5V2a2 2 0 114 0v.1a1.6 1.6 0 001 1.5 1.6 1.6 0 001.8-.3l.1-.1a2 2 0 112.8 2.8l-.1.1a1.6 1.6 0 00-.3 1.8V8a1.6 1.6 0 001.5 1H22a2 2 0 110 4h-.1a1.6 1.6 0 00-1.5 1Z" />,
};

const nav = [
  { to: "/", label: "Dashboard", end: true, icon: "dash" },
  { to: "/domains", label: "Domains", end: false, icon: "domains" },
  { to: "/messages", label: "Messages", end: false, icon: "messages" },
  { to: "/events", label: "Events", end: false, icon: "events" },
  { to: "/users", label: "Admin users", end: false, icon: "users" },
  { to: "/settings", label: "Settings", end: false, icon: "settings" },
];

export default function Layout() {
  const navigate = useNavigate();
  const location = useLocation();
  return (
    <div className="flex min-h-[100dvh]">
      <aside className="sticky top-0 flex h-[100dvh] w-64 shrink-0 flex-col gap-2 border-r border-white/[0.06] bg-surface/60 p-4 backdrop-blur-xl">
        <div className="flex items-center gap-2.5 px-2 py-4">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-primary shadow-soft shadow-inner-hi">
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="white" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 5l9 7 9-7M3 5v14h18V5" />
            </svg>
          </div>
          <div>
            <div className="text-lg font-bold leading-none tracking-tight">Relay</div>
            <div className="text-[10px] uppercase tracking-eyebrow text-muted-foreground">Transactional Mail</div>
          </div>
        </div>

        <nav className="flex flex-1 flex-col gap-1">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) =>
                cn(
                  "group relative flex items-center gap-3 rounded-xl px-3 py-2 text-sm font-medium transition-all duration-300 ease-spring",
                  isActive
                    ? "bg-white/[0.06] text-foreground shadow-inner-hi ring-1 ring-inset ring-white/[0.06]"
                    : "text-muted-foreground hover:bg-white/[0.03] hover:text-foreground",
                )
              }
            >
              {({ isActive }) => (
                <>
                  <span
                    className={cn(
                      "absolute left-0 top-1/2 h-5 w-1 -translate-y-1/2 rounded-r-full bg-primary transition-all duration-300 ease-spring",
                      isActive ? "opacity-100" : "opacity-0",
                    )}
                  />
                  <span className={cn(isActive ? "text-primary" : "text-muted-foreground group-hover:text-foreground")}>
                    {icons[n.icon]}
                  </span>
                  {n.label}
                </>
              )}
            </NavLink>
          ))}
        </nav>

        <button
          onClick={async () => {
            try {
              await logout();
            } catch {
              /* best-effort */
            }
            clearToken();
            navigate("/login", { replace: true });
          }}
          className="flex items-center gap-3 rounded-xl px-3 py-2 text-left text-sm font-medium text-muted-foreground transition-all duration-300 ease-spring hover:bg-white/[0.03] hover:text-foreground"
        >
          <I d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4M16 17l5-5-5-5M21 12H9" />
          Sign out
        </button>
      </aside>

      <main className="flex-1 overflow-auto">
        <div key={location.pathname} className="animate-rise mx-auto max-w-6xl px-6 py-10 md:px-10">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
