import { describe, expect, it, vi } from "vitest";
import { ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { getMatches } from "./matches.js";

const MARKET = "ETH-USDT";

function stubTransport(response: unknown) {
  const request = vi.fn().mockResolvedValue(response);
  return { transport: { request } as unknown as Transport, request };
}

describe("matches.getMatches", () => {
  it("requests the matches endpoint with no query params by default", async () => {
    const { transport, request } = stubTransport([]);

    await getMatches(transport, MARKET);

    expect(request).toHaveBeenCalledWith(
      "GET",
      "/api/v1/markets/ETH-USDT/matches",
      expect.objectContaining({ query: {} }),
    );
  });

  it("passes startDate/endDate/limit as query params", async () => {
    const { transport, request } = stubTransport([]);

    await getMatches(transport, MARKET, { startDate: "2026-01-01", endDate: "2026-01-02", limit: 20 });

    expect(request).toHaveBeenCalledWith(
      "GET",
      "/api/v1/markets/ETH-USDT/matches",
      expect.objectContaining({
        query: { start_date: "2026-01-01", end_date: "2026-01-02", limit: 20 },
      }),
    );
  });

  it("parses matches, decoding price/quantity as bigint", async () => {
    const { transport } = stubTransport([
      { id: "m1", price: 2010000000, quantity: 500000, taker_side: "buy", match_time: 1700000000 },
    ]);

    const matches = await getMatches(transport, MARKET);

    expect(matches).toEqual([
      { id: "m1", price: 2010000000n, quantity: 500000n, takerSide: "buy", matchTime: 1700000000 },
    ]);
  });

  it("rejects an empty market", async () => {
    const { transport } = stubTransport([]);
    await expect(getMatches(transport, "")).rejects.toBeInstanceOf(ValidationError);
  });

  it("rejects a limit outside 1-100", async () => {
    const { transport } = stubTransport([]);
    await expect(getMatches(transport, MARKET, { limit: 101 })).rejects.toBeInstanceOf(ValidationError);
    await expect(getMatches(transport, MARKET, { limit: 0 })).rejects.toBeInstanceOf(ValidationError);
  });

  it("rejects a malformed startDate/endDate", async () => {
    const { transport } = stubTransport([]);
    await expect(getMatches(transport, MARKET, { startDate: "not-a-date" })).rejects.toBeInstanceOf(
      ValidationError,
    );
    await expect(getMatches(transport, MARKET, { endDate: "01-01-2026" })).rejects.toBeInstanceOf(
      ValidationError,
    );
  });
});
