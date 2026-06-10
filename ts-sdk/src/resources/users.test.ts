import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { login, register } from "./users.js";

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

describe("users.login", () => {
  it("returns the token from the response", async () => {
    const { transport } = stubTransport({ token: "jwt-123" });
    await expect(login(transport, { username: "u", password: "pw" })).resolves.toBe(
      "jwt-123",
    );
  });

  it("throws ParseError when the response has no token", async () => {
    const { transport } = stubTransport({});
    await expect(login(transport, { username: "u", password: "pw" })).rejects.toBeInstanceOf(
      ParseError,
    );
  });

  it("validates credentials first", async () => {
    const { transport, request } = stubTransport({ token: "x" });
    await expect(login(transport, { username: "u", password: "" })).rejects.toBeInstanceOf(
      ValidationError,
    );
    expect(request).not.toHaveBeenCalled();
  });
});
