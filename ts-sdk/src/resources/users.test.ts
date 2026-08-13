import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { getBalances, getOperations, register } from "./users.js";

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
    frozen: 0n,
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

describe("users.getOperations", () => {
  const operationRow = {
    id: "0190f...",
    symbol: "ETH",
    amount: 1000000000000000000n,
    type: "freeze",
    reason: "KYC hold",
    created_at: 1700000000,
  };

  it("returns parsed operations, reason included", async () => {
    const { transport } = stubTransport([operationRow]);
    const result = await getOperations(transport, "tok");
    expect(result).toHaveLength(1);
    expect(result[0]?.type).toBe("freeze");
    expect(result[0]?.amount).toBe(1000000000000000000n);
    expect(result[0]?.reason).toBe("KYC hold");
  });

  it("omits reason when the server didn't send one", async () => {
    const { reason: _reason, ...withoutReason } = operationRow;
    const { transport } = stubTransport([withoutReason]);
    const result = await getOperations(transport, "tok");
    expect(result[0]?.reason).toBeUndefined();
  });

  it("forwards the bearer token and query filters", async () => {
    const { transport, request } = stubTransport([operationRow]);
    await getOperations(transport, "my-token", { startDate: "2026-01-01", limit: 50 });
    expect(request).toHaveBeenCalledWith("GET", "/api/v1/users/operations", {
      token: "my-token",
      query: { start_date: "2026-01-01", end_date: undefined, limit: 50 },
    });
  });

  it("validates the filter before calling the API", async () => {
    const { transport, request } = stubTransport();
    await expect(getOperations(transport, "tok", { limit: 0 })).rejects.toBeInstanceOf(
      ValidationError,
    );
    expect(request).not.toHaveBeenCalled();
  });

  it("throws ParseError when an operation entry is malformed", async () => {
    const { transport } = stubTransport([{ id: "op1" }]);
    await expect(getOperations(transport, "tok")).rejects.toBeInstanceOf(ParseError);
  });

  it("returns an empty array when the server sends []", async () => {
    const { transport } = stubTransport([]);
    await expect(getOperations(transport, "tok")).resolves.toEqual([]);
  });
});

