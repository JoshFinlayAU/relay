/** @type {import('tailwindcss').Config} */
export default {
  darkMode: "class",
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        border: "hsl(var(--border))",
        background: "hsl(var(--background))",
        surface: "hsl(var(--surface))",
        foreground: "hsl(var(--foreground))",
        muted: "hsl(var(--muted))",
        "muted-foreground": "hsl(var(--muted-foreground))",
        card: "hsl(var(--card))",
        elevated: "hsl(var(--elevated))",
        primary: "hsl(var(--primary))",
        "primary-foreground": "hsl(var(--primary-foreground))",
        destructive: "hsl(var(--destructive))",
        accent: "hsl(var(--accent))",
        emerald: "hsl(var(--emerald))",
        amber: "hsl(var(--amber))",
        rose: "hsl(var(--rose))",
      },
      fontFamily: {
        sans: ['"Plus Jakarta Sans Variable"', "ui-sans-serif", "system-ui", "sans-serif"],
        mono: ['"JetBrains Mono Variable"', "ui-monospace", "monospace"],
      },
      borderRadius: {
        "4xl": "2rem",
      },
      boxShadow: {
        // Soft, highly diffused ambient depth (no harsh dark drop shadows).
        soft: "0 1px 2px hsl(var(--shadow-color) / 0.4), 0 8px 24px -8px hsl(var(--shadow-color) / 0.5)",
        lift: "0 2px 4px hsl(var(--shadow-color) / 0.4), 0 24px 48px -16px hsl(var(--shadow-color) / 0.65)",
        "inner-hi": "inset 0 1px 0 0 hsl(var(--hairline) / 0.08)",
      },
      letterSpacing: {
        eyebrow: "0.2em",
      },
      transitionTimingFunction: {
        spring: "cubic-bezier(0.32, 0.72, 0, 1)",
      },
    },
  },
  plugins: [],
};
