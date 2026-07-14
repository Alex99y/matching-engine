// Sessions resource: create (login) and revoke (logout) sessions.

import type { Transport } from "../http/transport.js";
import type { LoginParams } from "../types/index.js";
import { parseLoginToken } from "../utils/parse.js";
import { validateLoginParams } from "../utils/validation.js";

const SESSIONS_BASE = "/api/v1/sessions";

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
