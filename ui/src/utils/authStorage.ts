import type { ApiTarget } from "../config.ts";

// Session persistence across page reloads.
//
// TODO: localStorage is the wrong long-term home for a bearer token — any XSS on
// this page can read it, and the browser hands it out to every script on the
// origin. The right shape is an httpOnly, Secure, SameSite cookie set by the API,
// which script can't read at all. That is NOT a UI-only change, which is why it
// isn't done here — it needs, on the API side:
//   1. `Set-Cookie` on POST /sessions (Login) and cookie clearing on DELETE /sessions.
//   2. A cookie fallback in the auth middleware, which today only reads the
//      Authorization header (api/pkg/middleware).
//   3. CORS with credentials + `SameSite=None; Secure` while the UI and API sit on
//      different origins (vite on :5173, API on :4000), or a gateway putting both
//      behind one origin so `SameSite=Lax` works.
//   4. CSRF protection (double-submit token or origin check), because unlike a
//      header, a cookie is attached automatically to cross-site requests.
// Until then this is a PoC-grade trade-off: refresh-persistence in exchange for a
// token readable by script.

export interface StoredAuth extends ApiTarget {
  token: string;
  username: string;
}

const STORAGE_KEY = "me.auth.v1";

export function loadStoredAuth(): StoredAuth | null {
  let raw: string | null;
  try {
    raw = localStorage.getItem(STORAGE_KEY);
  } catch {
    // Storage disabled (private mode, blocked cookies) — behave as if empty.
    return null;
  }
  if (raw === null) return null;

  try {
    const parsed: unknown = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return null;
    const o = parsed as Record<string, unknown>;
    if (
      typeof o["token"] !== "string" || o["token"] === "" ||
      typeof o["username"] !== "string" ||
      typeof o["host"] !== "string" || o["host"] === "" ||
      typeof o["port"] !== "number" ||
      typeof o["insecure"] !== "boolean"
    ) {
      return null;
    }
    return {
      token: o["token"],
      username: o["username"],
      host: o["host"],
      port: o["port"],
      insecure: o["insecure"],
    };
  } catch {
    return null;
  }
}

export function saveStoredAuth(auth: StoredAuth): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(auth));
  } catch {
    // A session that can't be persisted is still perfectly usable in this tab.
  }
}

export function clearStoredAuth(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // nothing to do — see saveStoredAuth
  }
}
