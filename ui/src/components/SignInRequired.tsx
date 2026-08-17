import { Link } from "react-router-dom";

// Shown on routes that need an authenticated session (history, faucet) when
// browsing as a guest. Guests can still reach these routes by URL even
// though the nav links are hidden for them, so each page needs a graceful
// fallback rather than assuming useSession() is safe to call.
export function SignInRequired({ what }: { what: string }) {
  return (
    <div style={s.container}>
      <span style={s.text}>Sign in to view {what}.</span>
      <Link to="/" style={s.link}>← Back to trading</Link>
    </div>
  );
}

const s = {
  container: {
    flex: 1,
    display: "flex",
    flexDirection: "column" as const,
    alignItems: "center",
    justifyContent: "center",
    gap: 10,
    color: "var(--text-muted)",
    fontSize: 13,
  },
  text: {},
  link: {
    color: "var(--accent)",
    fontSize: 12,
    textDecoration: "none",
  },
} as const;
