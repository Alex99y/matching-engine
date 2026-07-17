const LEVELS = { debug: 0, info: 1, warn: 2, error: 3 } as const;
type Level = keyof typeof LEVELS;

function parseLevel(s: string | undefined): number {
  if (s && s in LEVELS) return LEVELS[s as Level];
  return LEVELS.info;
}

const minLevel = parseLevel(process.env["LOG_LEVEL"]);

function emit(level: Level, msg: string, data?: Record<string, unknown>): void {
  if (LEVELS[level] < minLevel) return;
  const ts = new Date().toISOString();
  const line = `[${ts}] ${level.toUpperCase().padEnd(5)} ${msg}`;
  if (data !== undefined) {
    console.log(line, data);
  } else {
    console.log(line);
  }
}

export const logger = {
  debug: (msg: string, data?: Record<string, unknown>) => emit("debug", msg, data),
  info:  (msg: string, data?: Record<string, unknown>) => emit("info",  msg, data),
  warn:  (msg: string, data?: Record<string, unknown>) => emit("warn",  msg, data),
  error: (msg: string, data?: Record<string, unknown>) => emit("error", msg, data),
};
