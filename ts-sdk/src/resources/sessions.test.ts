import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { login, logout } from "./sessions.js";

function stubTransport(result: unknown = undefined) {
  const request = vi.fn().mockResolvedValue(result);
  return { transport: { request } as unknown as Transport, request };
}

describe("sessions.login", () => {
  it("posts credentials and returns the token", async () => {
    const { transport } = stubTransport({ token: "tok-abc" });
    await expect(login(transport, { username: "u", password: "pw" })).resolves.toBe("tok-abc");
  });

  it("sends to POST /api/v1/sessions", async () => {
    const { transport, request } = stubTransport({ token: "x" });
    await login(transport, { username: "u", password: "pw" });
    expect(request).toHaveBeenCalledWith("POST", "/api/v1/sessions", {
      body: { username: "u", password: "pw" },
    });
  });

  it("throws ValidationError when username is empty", async () => {
    const { transport, request } = stubTransport({ token: "x" });
    await expect(login(transport, { username: "", password: "pw" })).rejects.toBeInstanceOf(
      ValidationError,
    );
    expect(request).not.toHaveBeenCalled();
  });

  it("throws ValidationError when password is empty", async () => {
    const { transport, request } = stubTransport({ token: "x" });
    await expect(login(transport, { username: "u", password: "" })).rejects.toBeInstanceOf(
      ValidationError,
    );
    expect(request).not.toHaveBeenCalled();
  });

  it("throws ParseError when the response has no token field", async () => {
    const { transport } = stubTransport({});
    await expect(login(transport, { username: "u", password: "pw" })).rejects.toBeInstanceOf(
      ParseError,
    );
  });
});

describe("sessions.logout", () => {
  it("sends DELETE /api/v1/sessions with the bearer token", async () => {
    const { transport, request } = stubTransport(undefined);
    await logout(transport, "my-token");
    expect(request).toHaveBeenCalledWith("DELETE", "/api/v1/sessions", { token: "my-token" });
  });

  it("resolves to undefined on success", async () => {
    const { transport } = stubTransport(undefined);
    await expect(logout(transport, "tok")).resolves.toBeUndefined();
  });
});
