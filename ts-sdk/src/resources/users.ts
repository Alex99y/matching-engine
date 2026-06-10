// Users resource: register, login (public) and balance query (authenticated).

import type { Transport } from "../http/transport.js";
import type { Balance, LoginParams, RegisterParams } from "../types/index.js";
import { parseBalances, parseLoginToken } from "../utils/parse.js";
import {
  validateLoginParams,
  validateRegisterParams,
} from "../utils/validation.js";

const REGISTER_PATH = "/api/v1/users/register";
const LOGIN_PATH = "/api/v1/users/login";
const BALANCES_PATH = "/api/v1/users/balances";

export async function register(
  transport: Transport,
  params: RegisterParams,
): Promise<void> {
  validateRegisterParams(params);
  await transport.request<void>("POST", REGISTER_PATH, {
    body: {
      username: params.username,
      email: params.email,
      password: params.password,
    },
  });
}

/** Returns the bearer token on success. */
export async function login(
  transport: Transport,
  params: LoginParams,
): Promise<string> {
  validateLoginParams(params);
  const raw = await transport.request<unknown>("POST", LOGIN_PATH, {
    body: { username: params.username, password: params.password },
  });
  return parseLoginToken(raw);
}

/** Returns all instrument balances for the authenticated user. */
export async function getBalances(
  transport: Transport,
  token: string,
): Promise<Balance[]> {
  const raw = await transport.request<unknown>("GET", BALANCES_PATH, { token });
  return parseBalances(raw);
}
