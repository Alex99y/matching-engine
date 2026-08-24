export interface DepthUpdate {
  readonly lastUpdateId: number;
  readonly bids: readonly [string, string][];
  readonly asks: readonly [string, string][];
}

export type DepthHandler = (update: DepthUpdate) => void;
export type ErrorHandler = (err: Error) => void;

export interface DepthStream {
  start(): void;
  stop(): void;
}
