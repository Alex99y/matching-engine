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

export {
  CandleInterval,
  OperationType,
  OrderSide,
  OrderStatus,
  OrderType,
  SessionOrigin,
  SessionScope,
  TimeInForce,
} from "./types/index.js";
export type {
  Balance,
  BatchCancelOrderResponse,
  BatchCancelOrderResult,
  BatchCreateOrderResponse,
  BatchCreateOrderResult,
  BookLevel,
  BookMessage,
  Candle,
  CancelledOrder,
  CandleClosedMessage,
  CandleSnapshotMessage,
  CandleStreamMessage,
  CandleStreamOptions,
  CandleTradeMessage,
  CreateOrderParams,
  CreateTokenParams,
  CreateTokenResult,
  FaucetResult,
  GetCandlesParams,
  GetCandlesResponse,
  GetDepthOptions,
  GetMatchesFilter,
  GetOrdersFilter,
  GetUserOperationsFilter,
  HeartbeatMessage,
  Instrument,
  LoginParams,
  Market,
  MarketDepth,
  MarketStreamOptions,
  Match,
  OpenOrder,
  Operation,
  Order,
  OrderMessage,
  RefreshSessionResult,
  RegisterParams,
  Session,
  SnapshotMessage,
  StreamMessage,
  TradeMessage,
  UserStreamOptions,
} from "./types/index.js";
