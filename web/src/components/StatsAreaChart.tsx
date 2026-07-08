import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

export interface Point {
  t: string;
  delivered: number;
  deferred: number;
  bounced: number;
}

// Recharts is heavy; this module is lazy-loaded so it stays out of the main bundle.
export default function StatsAreaChart({ data }: { data: Point[] }) {
  return (
    <div style={{ width: "100%", height: 220 }}>
      <ResponsiveContainer>
        <AreaChart data={data} margin={{ top: 5, right: 10, left: -20, bottom: 0 }}>
          <defs>
            <linearGradient id="gDel" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="hsl(158 72% 45%)" stopOpacity={0.45} />
              <stop offset="100%" stopColor="hsl(158 72% 45%)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid strokeDasharray="3 3" stroke="hsl(230 16% 18%)" />
          <XAxis dataKey="t" tick={{ fontSize: 10, fill: "hsl(228 12% 64%)" }} minTickGap={40} />
          <YAxis allowDecimals={false} tick={{ fontSize: 10, fill: "hsl(228 12% 64%)" }} width={40} />
          <Tooltip
            contentStyle={{
              background: "hsl(230 20% 13%)",
              border: "1px solid hsl(230 16% 18%)",
              borderRadius: 12,
              fontSize: 12,
            }}
          />
          <Area type="monotone" dataKey="delivered" stroke="hsl(158 72% 45%)" strokeWidth={2} fill="url(#gDel)" />
          <Area type="monotone" dataKey="deferred" stroke="hsl(32 94% 58%)" strokeWidth={1.5} fillOpacity={0} />
          <Area type="monotone" dataKey="bounced" stroke="hsl(350 82% 63%)" strokeWidth={1.5} fillOpacity={0} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}
