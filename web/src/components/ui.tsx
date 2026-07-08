import { ButtonHTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/utils";
import type { CheckResult, DomainStatus } from "../api/domains";

const spring = "transition-all duration-300 ease-spring";

export function Button({
  className,
  variant = "primary",
  icon,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "destructive" | "ghost";
  icon?: ReactNode;
}) {
  const variants: Record<string, string> = {
    primary:
      "bg-primary text-primary-foreground shadow-soft hover:brightness-110 shadow-inner-hi",
    secondary:
      "bg-white/[0.04] text-foreground ring-1 ring-inset ring-white/10 hover:bg-white/[0.07]",
    destructive: "bg-destructive/90 text-white hover:bg-destructive",
    ghost: "text-muted-foreground hover:text-foreground hover:bg-white/[0.05]",
  };
  return (
    <button
      className={cn(
        "group inline-flex items-center justify-center gap-2 rounded-full px-5 py-2 text-sm font-semibold",
        spring,
        "active:scale-[0.97] disabled:opacity-50 disabled:pointer-events-none",
        variants[variant],
        className,
      )}
      {...props}
    >
      {children}
      {icon && (
        <span
          className={cn(
            "flex h-6 w-6 items-center justify-center rounded-full bg-black/10 dark:bg-white/15",
            "transition-transform duration-300 ease-spring group-hover:translate-x-0.5 group-hover:-translate-y-px group-hover:scale-105",
          )}
        >
          {icon}
        </span>
      )}
    </button>
  );
}

// Card - "double-bezel": an outer machined shell wrapping an inner core.
export function Card({
  children,
  className,
  bezel,
}: {
  children: ReactNode;
  className?: string;
  bezel?: boolean;
}) {
  if (bezel) {
    return (
      <div className="rounded-4xl bg-white/[0.03] p-1.5 ring-1 ring-inset ring-white/[0.06] shadow-soft">
        <div className={cn("rounded-[calc(2rem-0.375rem)] bg-card shadow-inner-hi ring-1 ring-inset ring-white/[0.04]", className)}>
          {children}
        </div>
      </div>
    );
  }
  return (
    <div
      className={cn(
        "rounded-2xl bg-card ring-1 ring-inset ring-white/[0.06] shadow-soft shadow-inner-hi",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function Eyebrow({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center rounded-full bg-white/[0.05] px-3 py-1 text-[10px] font-semibold uppercase tracking-eyebrow text-muted-foreground ring-1 ring-inset ring-white/10">
      {children}
    </span>
  );
}

export function PageHeader({ title, eyebrow, actions }: { title: string; eyebrow?: string; actions?: ReactNode }) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-4">
      <div className="space-y-2">
        {eyebrow && <Eyebrow>{eyebrow}</Eyebrow>}
        <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
      </div>
      {actions && <div className="flex gap-2">{actions}</div>}
    </div>
  );
}

export function StatTile({ label, value, accent, hint }: { label: string; value: ReactNode; accent?: string; hint?: string }) {
  return (
    <Card className="p-5">
      <div className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">{label}</div>
      <div className={cn("mt-2 text-3xl font-bold tabular-nums tracking-tight", accent)}>{value}</div>
      {hint && <div className="mt-1 text-xs text-muted-foreground">{hint}</div>}
    </Card>
  );
}

const statusColors: Record<DomainStatus, string> = {
  active: "bg-emerald/15 text-emerald ring-emerald/30",
  pending: "bg-amber/15 text-amber ring-amber/30",
  degraded: "bg-amber/15 text-amber ring-amber/30",
  suspended: "bg-rose/15 text-rose ring-rose/30",
};

function Dot({ className }: { className?: string }) {
  return <span className={cn("h-1.5 w-1.5 rounded-full", className)} />;
}

export function StatusBadge({ status }: { status: DomainStatus }) {
  const dot: Record<DomainStatus, string> = {
    active: "bg-emerald",
    pending: "bg-amber",
    degraded: "bg-amber",
    suspended: "bg-rose",
  };
  return (
    <span
      data-testid="domain-status"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-medium capitalize ring-1 ring-inset",
        statusColors[status],
      )}
    >
      <Dot className={dot[status]} />
      {status}
    </span>
  );
}

const checkColors: Record<CheckResult, string> = {
  pass: "bg-emerald/15 text-emerald ring-emerald/30",
  fail: "bg-rose/15 text-rose ring-rose/30",
  warn: "bg-amber/15 text-amber ring-amber/30",
  unknown: "bg-white/5 text-muted-foreground ring-white/10",
};

export function CheckBadge({ result }: { result: CheckResult }) {
  return (
    <span
      className={cn(
        "inline-flex rounded-full px-2.5 py-0.5 text-xs font-medium capitalize ring-1 ring-inset",
        checkColors[result],
      )}
    >
      {result}
    </span>
  );
}

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <label className="flex cursor-pointer items-center gap-3 text-sm">
      <span className="relative inline-flex h-6 w-10 shrink-0">
        <input
          type="checkbox"
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
          aria-label={label}
          className="peer absolute inset-0 z-10 h-full w-full cursor-pointer opacity-0"
        />
        <span className="pointer-events-none h-6 w-10 rounded-full bg-white/10 ring-1 ring-inset ring-white/10 transition-colors duration-300 ease-spring peer-checked:bg-primary peer-focus-visible:ring-2 peer-focus-visible:ring-primary/70" />
        <span className="pointer-events-none absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-soft transition-transform duration-300 ease-spring peer-checked:translate-x-4" />
      </span>
      {label}
    </label>
  );
}

export function CopyButton({ text }: { text: string }) {
  return (
    <Button
      variant="ghost"
      className="px-2.5 py-1 text-xs"
      title="Copy"
      onClick={() => void navigator.clipboard?.writeText(text).catch(() => {})}
    >
      Copy
    </Button>
  );
}
