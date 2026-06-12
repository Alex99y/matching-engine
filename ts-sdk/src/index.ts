// Single public entry point of the SDK. Anything not re-exported here is
// internal and must not be imported via deep paths by consumers.

export { MatchingEngineClient } from "./client/matching-engine-client.js";
export type { ClientOptions } from "./client/matching-engine-client.js";
export { AuthenticatedClient } from "./client/authenticated-client.js";

export {
  SDKError,
  NetworkError,
  TimeoutError,
  APIError,
  AuthenticationError,
  RateLimitError,
  ValidationError,
  ParseError,
} from "./errors/index.js";

export { OrderSide, OrderType, TimeInForce } from "./types/index.js";
export type {
  Balance,
  BatchCancelOrderResponse,
  BatchCancelOrderResult,
  BatchCreateOrderResponse,
  BatchCreateOrderResult,
  CancelledOrder,
  CreateOrderParams,
  GetOrdersFilter,
  Instrument,
  LoginParams,
  Market,
  OpenOrder,
  Order,
  RegisterParams,
} from "./types/index.js";
