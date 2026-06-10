// SDK error hierarchy. No raw runtime error (TypeError, fetch failure,
// JSON SyntaxError) is allowed to escape the public API: the http/ layer maps
// everything into one of these classes. `instanceof` works across the chain.

export class SDKError extends Error {
  // Error.cause already exists in the ES2022 lib; we set it only when provided
  // so the property stays absent otherwise.
  constructor(message: string, cause?: unknown) {
    super(message);
    this.name = new.target.name;
    if (cause !== undefined) {
      this.cause = cause;
    }
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

/** Network failure: DNS, connection refused, socket reset, etc. Retryable. */
export class NetworkError extends SDKError {}

/** Request aborted because it exceeded the configured timeout. Retryable. */
export class TimeoutError extends NetworkError {}

/** Non-2xx response from the server. */
export class APIError extends SDKError {
  readonly status: number;
  declare readonly body?: unknown;

  constructor(message: string, status: number, body?: unknown) {
    super(message);
    this.status = status;
    if (body !== undefined) {
      this.body = body;
    }
  }
}

/** 401 / 403. */
export class AuthenticationError extends APIError {}

/** 429. Carries the parsed Retry-After delay when the server provides one. */
export class RateLimitError extends APIError {
  declare readonly retryAfterMs?: number;

  constructor(
    message: string,
    status: number,
    retryAfterMs?: number,
    body?: unknown,
  ) {
    super(message, status, body);
    if (retryAfterMs !== undefined) {
      this.retryAfterMs = retryAfterMs;
    }
  }
}

/** Invalid input detected client-side, before any request is sent. */
export class ValidationError extends SDKError {}

/** Response had an unexpected shape or could not be parsed. */
export class ParseError extends SDKError {}
