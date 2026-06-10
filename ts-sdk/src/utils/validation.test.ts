import { describe, expect, it } from "vitest";
import { ValidationError } from "../errors/index.js";
import { OrderSide, OrderType, TimeInForce } from "../types/index.js";
import {
  validateCreateOrderParams,
  validateGetOrdersFilter,
  validateLoginParams,
  validateOrderId,
  validateRegisterParams,
} from "./validation.js";

const validCreate = {
  market: "ETH-USDT",
  side: OrderSide.Buy,
  type: OrderType.Limit,
  timeInForce: TimeInForce.GoodTillCancel,
} as const;

describe("validateRegisterParams", () => {
  it("passes valid input", () => {
    expect(() =>
      validateRegisterParams({ username: "u", email: "e@x.io", password: "p" }),
    ).not.toThrow();
  });

  it.each(["username", "email", "password"])(
    "throws when %s is empty",
    (field) => {
      const params = { username: "u", email: "e@x.io", password: "p", [field]: "" };
      expect(() => validateRegisterParams(params)).toThrow(ValidationError);
    },
  );
});

describe("validateLoginParams", () => {
  it("throws when password is empty", () => {
    expect(() => validateLoginParams({ username: "u", password: "" })).toThrow(
      ValidationError,
    );
  });
});

describe("validateOrderId", () => {
  it("throws on empty id", () => {
    expect(() => validateOrderId("")).toThrow(ValidationError);
  });
});

describe("validateCreateOrderParams", () => {
  it("passes valid input", () => {
    expect(() => validateCreateOrderParams(validCreate)).not.toThrow();
  });

  it("throws on empty market", () => {
    expect(() => validateCreateOrderParams({ ...validCreate, market: "" })).toThrow(
      ValidationError,
    );
  });

  it("throws on invalid side / type / tif", () => {
    expect(() =>
      validateCreateOrderParams({ ...validCreate, side: "up" as OrderSide }),
    ).toThrow(ValidationError);
    expect(() =>
      validateCreateOrderParams({ ...validCreate, type: "stop" as OrderType }),
    ).toThrow(ValidationError);
    expect(() =>
      validateCreateOrderParams({ ...validCreate, timeInForce: "x" as TimeInForce }),
    ).toThrow(ValidationError);
  });

  it("throws when clientOrderId length is out of range", () => {
    expect(() =>
      validateCreateOrderParams({ ...validCreate, clientOrderId: "short" }),
    ).toThrow(ValidationError);
  });

  it("accepts a clientOrderId of valid length", () => {
    expect(() =>
      validateCreateOrderParams({ ...validCreate, clientOrderId: "a".repeat(32) }),
    ).not.toThrow();
  });

  it("throws on negative amounts", () => {
    expect(() =>
      validateCreateOrderParams({ ...validCreate, price: -1n }),
    ).toThrow(ValidationError);
    expect(() =>
      validateCreateOrderParams({ ...validCreate, quantity: -1n }),
    ).toThrow(ValidationError);
    expect(() =>
      validateCreateOrderParams({ ...validCreate, quoteQty: -1n }),
    ).toThrow(ValidationError);
  });
});

describe("validateGetOrdersFilter", () => {
  it("passes an empty filter", () => {
    expect(() => validateGetOrdersFilter({})).not.toThrow();
  });

  it.each([0, 101, 1.5])("throws on invalid limit %s", (limit) => {
    expect(() => validateGetOrdersFilter({ limit })).toThrow(ValidationError);
  });

  it("throws on malformed dates", () => {
    expect(() => validateGetOrdersFilter({ startDate: "2026/01/01" })).toThrow(
      ValidationError,
    );
    expect(() => validateGetOrdersFilter({ endDate: "bad" })).toThrow(ValidationError);
  });

  it("accepts well-formed dates and limit", () => {
    expect(() =>
      validateGetOrdersFilter({ startDate: "2026-01-01", endDate: "2026-02-01", limit: 50 }),
    ).not.toThrow();
  });
});
