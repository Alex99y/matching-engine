// Users resource: register (public) and balance query (authenticated).

import type { Transport } from "../http/transport.js";
import type { Balance, RegisterParams } from "../types/index.js";
import { parseBalances } from "../utils/parse.js";
import { validateRegisterParams } from "../utils/validation.js";

const REGISTER_PATH = "/api/v1/users/register";
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

/** Returns all instrument balances for the authenticated user. */
export async function getBalances(
  transport: Transport,
  token: string,
): Promise<Balance[]> {
  const raw = await transport.request<unknown>("GET", BALANCES_PATH, { token });
  return parseBalances(raw);
}
