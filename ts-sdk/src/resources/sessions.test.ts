import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { getActiveSessions, login, logout, revokeSession } from "./sessions.js";

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

describe("sessions.getActiveSessions", () => {
  it("sends GET /api/v1/sessions/active with the bearer token", async () => {
    const { transport, request } = stubTransport([]);
    await getActiveSessions(transport, "my-token");
    expect(request).toHaveBeenCalledWith("GET", "/api/v1/sessions/active", {
      token: "my-token",
    });
  });

  it("returns mapped sessions", async () => {
    const { transport } = stubTransport([
      { session_id: "hash-1", created_at: 1700000000, expires_at: 1700604800 },
    ]);
    const sessions = await getActiveSessions(transport, "tok");
    expect(sessions).toEqual([
      { sessionId: "hash-1", createdAt: 1700000000, expiresAt: 1700604800 },
    ]);
  });

  it("throws ParseError when the response is not an array", async () => {
    const { transport } = stubTransport({});
    await expect(getActiveSessions(transport, "tok")).rejects.toBeInstanceOf(ParseError);
  });
});

describe("sessions.revokeSession", () => {
  it("sends DELETE /api/v1/sessions/active with the session id and bearer token", async () => {
    const { transport, request } = stubTransport(undefined);
    await revokeSession(transport, "my-token", "hash-1");
    expect(request).toHaveBeenCalledWith("DELETE", "/api/v1/sessions/active", {
      token: "my-token",
      body: { session_id: "hash-1" },
    });
  });

  it("resolves to undefined on success", async () => {
    const { transport } = stubTransport(undefined);
    await expect(revokeSession(transport, "tok", "hash-1")).resolves.toBeUndefined();
  });

  it("throws ValidationError when sessionId is empty", async () => {
    const { transport, request } = stubTransport(undefined);
    await expect(revokeSession(transport, "tok", "")).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
  });
});
