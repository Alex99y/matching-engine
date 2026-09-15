import { describe, expect, it } from "vitest";
import { ValidationError } from "../errors/index.js";
import { OrderSide, OrderType, SessionScope, TimeInForce } from "../types/index.js";
import {
  validateCreateOrderParams,
  validateCreateTokenParams,
  validateGetOrdersFilter,
  validateGetUserOperationsFilter,
  validateLoginParams,
  validateOrderId,
  validateRegisterParams,
  validateSessionId,
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

describe("validateSessionId", () => {
  it("throws on empty id", () => {
    expect(() => validateSessionId("")).toThrow(ValidationError);
  });

  it("passes a non-empty id", () => {
    expect(() => validateSessionId("hash-1")).not.toThrow();
  });
});

describe("validateCreateTokenParams", () => {
  it.each([SessionScope.Read, SessionScope.Write])("passes scope %s", (scope) => {
    expect(() => validateCreateTokenParams({ scope })).not.toThrow();
  });

  it("throws on an invalid scope", () => {
    expect(() => validateCreateTokenParams({ scope: "admin" as never })).toThrow(
      ValidationError,
    );
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

  it("throws when postOnly is set on a non-limit or non-gtc order", () => {
    expect(() =>
      validateCreateOrderParams({
        ...validCreate,
        type: OrderType.Market,
        postOnly: true,
      }),
    ).toThrow(ValidationError);
    expect(() =>
      validateCreateOrderParams({
        ...validCreate,
        timeInForce: TimeInForce.ImmediateOrCancel,
        postOnly: true,
      }),
    ).toThrow(ValidationError);
  });

  it("accepts postOnly on a limit gtc order", () => {
    expect(() =>
      validateCreateOrderParams({ ...validCreate, postOnly: true }),
    ).not.toThrow();
  });

  it("throws when clientOrderId length is out of range", () => {
    expect(() =>
      validateCreateOrderParams({ ...validCreate, clientOrderId: "short" }),
    ).toThrow(ValidationError);
  });

  // The exit is opposite-side, so a limit buy at 100 takes profit above and stops below;
  // a sell is mirrored; a market entry is only checked for the two triggers' relative order.
  describe("bracket triggers", () => {
    const limitBuy = { ...validCreate, price: 100n, quantity: 5n };
    const limitSell = { ...limitBuy, side: OrderSide.Sell };
    const marketBuy = {
      ...validCreate,
      type: OrderType.Market,
      timeInForce: TimeInForce.ImmediateOrCancel,
      quoteQty: 1000n,
    };

    it.each([
      ["buy tp above price", { ...limitBuy, takeProfitPrice: 150n }],
      ["buy sl below price", { ...limitBuy, stopLossPrice: 50n }],
      ["buy both", { ...limitBuy, takeProfitPrice: 150n, stopLossPrice: 50n }],
      ["sell tp below price", { ...limitSell, takeProfitPrice: 50n }],
      ["sell sl above price", { ...limitSell, stopLossPrice: 150n }],
      ["sell both", { ...limitSell, takeProfitPrice: 50n, stopLossPrice: 150n }],
      ["market buy only needs sl below tp", { ...marketBuy, takeProfitPrice: 150n, stopLossPrice: 50n }],
    ])("accepts %s", (_name, params) => {
      expect(() => validateCreateOrderParams(params)).not.toThrow();
    });

    it.each([
      ["zero tp", { ...limitBuy, takeProfitPrice: 0n }],
      ["negative sl", { ...limitBuy, stopLossPrice: -1n }],
      ["buy tp at price", { ...limitBuy, takeProfitPrice: 100n }],
      ["buy tp below price", { ...limitBuy, takeProfitPrice: 90n }],
      ["buy sl at price", { ...limitBuy, stopLossPrice: 100n }],
      ["buy sl above price", { ...limitBuy, stopLossPrice: 110n }],
      ["sell tp above price", { ...limitSell, takeProfitPrice: 110n }],
      ["sell sl below price", { ...limitSell, stopLossPrice: 90n }],
      ["market buy sl above tp", { ...marketBuy, takeProfitPrice: 50n, stopLossPrice: 150n }],
      ["market buy sl equals tp", { ...marketBuy, takeProfitPrice: 100n, stopLossPrice: 100n }],
    ])("rejects %s", (_name, params) => {
      expect(() => validateCreateOrderParams(params)).toThrow(ValidationError);
    });
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

describe("validateGetUserOperationsFilter", () => {
  it("passes an empty filter", () => {
    expect(() => validateGetUserOperationsFilter({})).not.toThrow();
  });

  it.each([0, 101, 1.5])("throws on invalid limit %s", (limit) => {
    expect(() => validateGetUserOperationsFilter({ limit })).toThrow(ValidationError);
  });

  it("throws on malformed dates", () => {
    expect(() => validateGetUserOperationsFilter({ startDate: "2026/01/01" })).toThrow(
      ValidationError,
    );
    expect(() => validateGetUserOperationsFilter({ endDate: "bad" })).toThrow(ValidationError);
  });

  it("accepts well-formed dates and limit", () => {
    expect(() =>
      validateGetUserOperationsFilter({
        startDate: "2026-01-01",
        endDate: "2026-02-01",
        limit: 50,
      }),
    ).not.toThrow();
  });
});
