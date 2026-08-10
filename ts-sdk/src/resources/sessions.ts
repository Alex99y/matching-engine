// Sessions resource: create (login) and revoke (logout) sessions.

import type { Transport } from "../http/transport.js";
import type { LoginParams, Session } from "../types/index.js";
import { parseActiveSessions, parseLoginToken } from "../utils/parse.js";
import { validateLoginParams, validateSessionId } from "../utils/validation.js";

const SESSIONS_BASE = "/api/v1/sessions";
const ACTIVE_SESSIONS_PATH = `${SESSIONS_BASE}/active`;

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
 *
 * @throws {@link AuthenticationError} (401) when the token is already expired or revoked.
 */
export async function logout(transport: Transport, token: string): Promise<void> {
  await transport.request<void>("DELETE", SESSIONS_BASE, { token });
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
 *
 * @throws {@link ValidationError} when `sessionId` is empty.
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
