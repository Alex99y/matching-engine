// Users resource: register (public), balance query, and operation history
// (both authenticated).

import type { Transport } from "../http/transport.js";
import type {
  Balance,
  GetUserOperationsFilter,
  Operation,
  RegisterParams,
} from "../types/index.js";
import { parseBalances, parseOperations } from "../utils/parse.js";
import {
  validateGetUserOperationsFilter,
  validateRegisterParams,
} from "../utils/validation.js";

const REGISTER_PATH = "/api/v1/users/register";
const BALANCES_PATH = "/api/v1/users/balances";
const OPERATIONS_PATH = "/api/v1/users/operations";

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

/** Returns the authenticated user's deposit/withdraw/freeze/unfreeze history. */
export async function getOperations(
  transport: Transport,
  token: string,
  filter: GetUserOperationsFilter = {},
): Promise<Operation[]> {
  validateGetUserOperationsFilter(filter);

  const query: Record<string, string | number | undefined> = {
    start_date: filter.startDate,
    end_date: filter.endDate,
    limit: filter.limit,
  };

  const raw = await transport.request<unknown>("GET", OPERATIONS_PATH, {
    query,
    token,
  });
  return parseOperations(raw);
}
