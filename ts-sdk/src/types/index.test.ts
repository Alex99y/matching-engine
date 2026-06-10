import { describe, expect, it } from "vitest";
import { OrderSide, OrderType, TimeInForce } from "./index.js";

// These constants are the wire contract with the Go API. If a value changes
// here without a matching API change, orders will be rejected — so pin them.
describe("wire enums", () => {
  it("OrderSide values match the API", () => {
    expect(OrderSide).toEqual({ Buy: "buy", Sell: "sell" });
  });

  it("OrderType values match the API", () => {
    expect(OrderType).toEqual({ Limit: "limit", Market: "market" });
  });

  it("TimeInForce values match the API", () => {
    expect(TimeInForce).toEqual({
      GoodTillCancel: "gtc",
      ImmediateOrCancel: "ioc",
      FillOrKill: "fok",
    });
  });
});
