import { describe, expect, it, vi } from "vitest";
import { ParseError, ValidationError } from "../errors/index.js";
import type { Transport } from "../http/transport.js";
import { SessionScope } from "../types/index.js";
import {
  createToken,
  getActiveSessions,
  login,
  logout,
  refreshSession,
  revokeSession,
} from "./sessions.js";

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
      {
        session_id: "hash-1",
        created_at: 1700000000,
        expires_at: 1700604800,
        origin: "login",
        scope: "write",
      },
    ]);
    const sessions = await getActiveSessions(transport, "tok");
    expect(sessions).toEqual([
      {
        sessionId: "hash-1",
        createdAt: 1700000000,
        expiresAt: 1700604800,
        origin: "login",
        scope: "write",
      },
    ]);
  });

  it("includes userAgent/ipAddress only when present", async () => {
    const { transport } = stubTransport([
      {
        session_id: "hash-1",
        created_at: 1700000000,
        expires_at: 1700604800,
        origin: "minted",
        scope: "read",
        user_agent: "curl/8.0",
        ip_address: "127.0.0.1",
      },
    ]);
    const [session] = await getActiveSessions(transport, "tok");
    expect(session).toEqual({
      sessionId: "hash-1",
      createdAt: 1700000000,
      expiresAt: 1700604800,
      origin: "minted",
      scope: "read",
      userAgent: "curl/8.0",
      ipAddress: "127.0.0.1",
    });
  });

  it("throws ParseError when the response is not an array", async () => {
    const { transport } = stubTransport({});
    await expect(getActiveSessions(transport, "tok")).rejects.toBeInstanceOf(ParseError);
  });
});

describe("sessions.refreshSession", () => {
  it("sends POST /api/v1/sessions/refresh with the bearer token", async () => {
    const { transport, request } = stubTransport({ expires_at: 1700604800 });
    await refreshSession(transport, "my-token");
    expect(request).toHaveBeenCalledWith("POST", "/api/v1/sessions/refresh", {
      token: "my-token",
    });
  });

  it("returns the new expiry", async () => {
    const { transport } = stubTransport({ expires_at: 1700604800 });
    await expect(refreshSession(transport, "tok")).resolves.toEqual({ expiresAt: 1700604800 });
  });

  it("throws ParseError when the response has no expires_at field", async () => {
    const { transport } = stubTransport({});
    await expect(refreshSession(transport, "tok")).rejects.toBeInstanceOf(ParseError);
  });
});

describe("sessions.createToken", () => {
  it("sends POST /api/v1/sessions/tokens with the scope and bearer token", async () => {
    const { transport, request } = stubTransport({
      token: "minted-tok",
      scope: "read",
      expires_at: 1700604800,
    });
    await createToken(transport, "my-token", { scope: SessionScope.Read });
    expect(request).toHaveBeenCalledWith("POST", "/api/v1/sessions/tokens", {
      token: "my-token",
      body: { scope: "read" },
    });
  });

  it("returns the minted token, scope, and expiry", async () => {
    const { transport } = stubTransport({
      token: "minted-tok",
      scope: "write",
      expires_at: 1700604800,
    });
    await expect(createToken(transport, "tok", { scope: SessionScope.Write })).resolves.toEqual({
      token: "minted-tok",
      scope: "write",
      expiresAt: 1700604800,
    });
  });

  it("throws ValidationError for an invalid scope", async () => {
    const { transport, request } = stubTransport(undefined);
    await expect(
      createToken(transport, "tok", { scope: "admin" as never }),
    ).rejects.toBeInstanceOf(ValidationError);
    expect(request).not.toHaveBeenCalled();
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
