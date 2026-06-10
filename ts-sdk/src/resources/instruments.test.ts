import { describe, expect, it, vi } from "vitest";
import type { Transport } from "../http/transport.js";
import { getInstruments } from "./instruments.js";

describe("instruments.getInstruments", () => {
  it("requests the instruments endpoint and maps the response", async () => {
    const request = vi.fn().mockResolvedValue([
      { name: "Ether", symbol: "ETH", decimals: 18, created_at: "2026-01-01T00:00:00Z" },
    ]);
    const transport = { request } as unknown as Transport;

    const instruments = await getInstruments(transport);

    expect(request).toHaveBeenCalledWith("GET", "/api/v1/instruments/");
    expect(instruments[0]?.symbol).toBe("ETH");
    expect(instruments[0]?.createdAt).toBe("2026-01-01T00:00:00Z");
  });
});
