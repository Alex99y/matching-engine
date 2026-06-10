import { describe, expect, it } from "vitest";
import {
  APIError,
  AuthenticationError,
  NetworkError,
  ParseError,
  RateLimitError,
  SDKError,
  TimeoutError,
  ValidationError,
} from "./index.js";

describe("error hierarchy", () => {
  it("keeps instanceof working across the chain", () => {
    const err = new RateLimitError("slow down", 429, 1000);
    expect(err).toBeInstanceOf(RateLimitError);
    expect(err).toBeInstanceOf(APIError);
    expect(err).toBeInstanceOf(SDKError);
    expect(err).toBeInstanceOf(Error);
  });

  it("TimeoutError is a NetworkError", () => {
    const err = new TimeoutError("timed out");
    expect(err).toBeInstanceOf(NetworkError);
    expect(err).toBeInstanceOf(SDKError);
  });

  it("sets the class name as the error name", () => {
    expect(new ValidationError("x").name).toBe("ValidationError");
    expect(new ParseError("x").name).toBe("ParseError");
    expect(new AuthenticationError("x", 401).name).toBe("AuthenticationError");
  });

  it("preserves the cause", () => {
    const cause = new Error("root");
    expect(new NetworkError("wrap", cause).cause).toBe(cause);
  });

  it("omits cause when not provided", () => {
    expect("cause" in new SDKError("x")).toBe(false);
  });

  it("carries status and body on APIError", () => {
    const err = new APIError("nope", 404, { message: "not found" });
    expect(err.status).toBe(404);
    expect(err.body).toEqual({ message: "not found" });
  });

  it("carries retryAfterMs on RateLimitError", () => {
    expect(new RateLimitError("x", 429, 2500).retryAfterMs).toBe(2500);
    expect("retryAfterMs" in new RateLimitError("x", 429)).toBe(false);
  });
});
