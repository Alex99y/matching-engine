// Instruments resource: list tradable assets. Public (unauthenticated).

import type { Transport } from "../http/transport.js";
import type { Instrument } from "../types/index.js";
import { parseInstruments } from "../utils/parse.js";

const INSTRUMENTS_PATH = "/api/v1/instruments/";

export async function getInstruments(
  transport: Transport,
): Promise<Instrument[]> {
  const raw = await transport.request<unknown>("GET", INSTRUMENTS_PATH);
  return parseInstruments(raw);
}
