// SHA-256 hex digest of a raw bearer token. This is the exact derivation the API
// uses for a session's external id (common/pkg/token.Hash), so it lets the UI pick
// its own row out of the active-session list — the server never says which one is
// the caller's.
//
// Returns null when it can't be computed: crypto.subtle only exists in a secure
// context, so serving this UI over plain http on anything other than localhost
// leaves the "this device" marker off rather than breaking the page.
export async function sessionIdForToken(rawToken: string): Promise<string | null> {
  if (typeof crypto === "undefined" || crypto.subtle === undefined) return null;
  try {
    const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(rawToken));
    return Array.from(new Uint8Array(digest))
      .map((b) => b.toString(16).padStart(2, "0"))
      .join("");
  } catch {
    return null;
  }
}
