import { describe, expect, it } from "vitest";
import * as sdk from "./index.js";

describe("public API surface", () => {
  it("exports the client classes", () => {
    expect(typeof sdk.MatchingEngineClient).toBe("function");
    expect(typeof sdk.AuthenticatedClient).toBe("function");
  });

  it("exports the full error hierarchy", () => {
    for (const name of [
      "SDKError",
      "NetworkError",
      "TimeoutError",
      "APIError",
      "AuthenticationError",
      "RateLimitError",
      "ValidationError",
      "ParseError",
    ] as const) {
      expect(typeof sdk[name]).toBe("function");
    }
  });

  it("exports the wire enums", () => {
    expect(sdk.OrderSide.Buy).toBe("buy");
    expect(sdk.OrderType.Limit).toBe("limit");
    expect(sdk.TimeInForce.FillOrKill).toBe("fok");
  });
});
