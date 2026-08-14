import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { requestFaucetFunds } from "./faucet.js";

function stubTransport(result: unknown = undefined) {
  const request = vi.fn().mockResolvedValue(result);
  return { transport: { request } as unknown as Transport, request };
}

describe("faucet.requestFaucetFunds", () => {
  it("posts the instrument as a query param and forwards the token", async () => {
    const { transport, request } = stubTransport({ symbol: "BTC", amount: 1000000000n });
    const result = await requestFaucetFunds(transport, "my-token", "BTC");
    expect(result.symbol).toBe("BTC");
    expect(result.amount).toBe(1000000000n);
    expect(request).toHaveBeenCalledWith("POST", "/api/v1/faucet", {
      query: { instrument: "BTC" },
      token: "my-token",
    });
  });

  it("validates before calling the API", async () => {
    const { transport, request } = stubTransport();
    await expect(requestFaucetFunds(transport, "tok", "")).rejects.toBeInstanceOf(
      ValidationError,
    );
    expect(request).not.toHaveBeenCalled();
  });

  it("throws ParseError when the response is malformed", async () => {
    const { transport } = stubTransport({ symbol: "BTC" });
    await expect(requestFaucetFunds(transport, "tok", "BTC")).rejects.toBeInstanceOf(ParseError);
  });
});
