import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { getBalances, register } from "./users.js";

function stubTransport(result: unknown = undefined) {
  const request = vi.fn().mockResolvedValue(result);
  return { transport: { request } as unknown as Transport, request };
}

describe("users.register", () => {
  it("posts the registration body", async () => {
    const { transport, request } = stubTransport();
    await register(transport, { username: "u", email: "e@x.io", password: "pw" });
    expect(request).toHaveBeenCalledWith("POST", "/api/v1/users/register", {
      body: { username: "u", email: "e@x.io", password: "pw" },
    });
  });

  it("validates before calling the API", async () => {
    const { transport, request } = stubTransport();
    await expect(
      register(transport, { username: "", email: "e@x.io", password: "pw" }),
    ).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });
});

describe("users.getBalances", () => {
  const balanceRow = {
    name: "Ether",
    symbol: "ETH",
    decimals: 18,
    balance: 5000000000000000000n,
    blocked: 1000000000000000000n,
  };

  it("returns parsed balances", async () => {
    const { transport } = stubTransport([balanceRow]);
    const result = await getBalances(transport, "tok");
    expect(result).toHaveLength(1);
    expect(result[0]?.symbol).toBe("ETH");
    expect(result[0]?.balance).toBe(5000000000000000000n);
    expect(result[0]?.blocked).toBe(1000000000000000000n);
  });

  it("forwards the bearer token", async () => {
    const { transport, request } = stubTransport([balanceRow]);
    await getBalances(transport, "my-token");
    expect(request.mock.calls[0]?.[2]?.token).toBe("my-token");
  });

  it("throws ParseError when a balance entry is malformed", async () => {
    const { transport } = stubTransport([{ name: "ETH" }]);
    await expect(getBalances(transport, "tok")).rejects.toBeInstanceOf(ParseError);
  });

  it("returns an empty array when the server sends []", async () => {
    const { transport } = stubTransport([]);
    await expect(getBalances(transport, "tok")).resolves.toEqual([]);
  });
});

