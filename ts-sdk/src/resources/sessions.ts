// Sessions resource: create (login), revoke (logout), refresh, list, and mint
// scoped tokens.

import type { Transport } from "../http/transport.js";
import type {
  CreateTokenParams,
  CreateTokenResult,
  LoginParams,
  RefreshSessionResult,
  Session,
} from "../types/index.js";
import {
  parseActiveSessions,
  parseCreateTokenResult,
  parseLoginToken,
  parseRefreshSessionResult,
} from "../utils/parse.js";
import {
  validateCreateTokenParams,
  validateLoginParams,
  validateSessionId,
} from "../utils/validation.js";

const SESSIONS_BASE = "/api/v1/sessions";
const ACTIVE_SESSIONS_PATH = `${SESSIONS_BASE}/active`;
const REFRESH_SESSION_PATH = `${SESSIONS_BASE}/refresh`;
const CREATE_TOKEN_PATH = `${SESSIONS_BASE}/tokens`;

/**
 * Authenticate and return the bearer token on success.
 *
 * @throws {@link ValidationError} when `username` or `password` is empty.
 * @throws {@link AuthenticationError} (401) on bad credentials.
 */
export async function login(transport: Transport, params: LoginParams): Promise<string> {
  validateLoginParams(params);
  const raw = await transport.request<unknown>("POST", SESSIONS_BASE, {
    body: { username: params.username, password: params.password },
  });
  return parseLoginToken(raw);
}

/**
 * Revoke the current session. After this call the bearer token is invalid.
 * Allowed for any session (login or minted) — it can only ever revoke itself.
 *
 * @throws {@link AuthenticationError} (401) when the token is already expired or revoked.
 */
export async function logout(transport: Transport, token: string): Promise<void> {
  await transport.request<void>("DELETE", SESSIONS_BASE, { token });
}

/**
 * Extend the current session's expiry by the server's standard TTL, without
 * changing the token value. Subject to an absolute age cap enforced server-side —
 * eventually a session must re-authenticate via {@link login} regardless of how
 * often it's refreshed.
 *
 * @throws {@link AuthenticationError} (401) when the session can no longer be
 * refreshed (expired, revoked, or past the absolute age cap) — log in again.
 */
export async function refreshSession(
  transport: Transport,
  token: string,
): Promise<RefreshSessionResult> {
  const raw = await transport.request<unknown>("POST", REFRESH_SESSION_PATH, { token });
  return parseRefreshSessionResult(raw);
}

/**
 * Mint a new scoped token on behalf of the caller. Restricted to login-origin
 * sessions — a minted token can never mint another one.
 *
 * @throws {@link ValidationError} when `scope` is not "read" or "write".
 * @throws {@link AuthenticationError} (403) when called from a minted (non-login) session.
 */
export async function createToken(
  transport: Transport,
  token: string,
  params: CreateTokenParams,
): Promise<CreateTokenResult> {
  validateCreateTokenParams(params);
  const raw = await transport.request<unknown>("POST", CREATE_TOKEN_PATH, {
    token,
    body: { scope: params.scope },
  });
  return parseCreateTokenResult(raw);
}

/**
 * List the authenticated user's active (non-expired, non-revoked) sessions.
 *
 * @throws {@link AuthenticationError} (401) when the token is invalid or expired.
 */
export async function getActiveSessions(transport: Transport, token: string): Promise<Session[]> {
  const raw = await transport.request<unknown>("GET", ACTIVE_SESSIONS_PATH, { token });
  return parseActiveSessions(raw);
}

/**
 * Revoke one of the authenticated user's active sessions by its session id
 * (as returned by {@link getActiveSessions}) — not necessarily the caller's
 * own session. Unlike {@link logout}, which always revokes the session
 * behind the current bearer token, this can end a session on another device.
 * Restricted to login-origin sessions — a minted token can never revoke a
 * session other than itself (use {@link logout} for that).
 *
 * @throws {@link ValidationError} when `sessionId` is empty.
 * @throws {@link AuthenticationError} (403) when called from a minted (non-login) session.
 * @throws {@link APIError} (404) when no active session matches `sessionId` for this user.
 */
export async function revokeSession(
  transport: Transport,
  token: string,
  sessionId: string,
): Promise<void> {
  validateSessionId(sessionId);
  await transport.request<void>("DELETE", ACTIVE_SESSIONS_PATH, {
    token,
    body: { session_id: sessionId },
  });
}
