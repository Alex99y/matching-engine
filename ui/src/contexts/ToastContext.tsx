import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";

// ── Types ─────────────────────────────────────────────────────────────────

type ToastType = "success" | "error" | "info";

interface Toast {
  id: string;
  message: string;
  type: ToastType;
  dying: boolean;
}

interface ToastContextValue {
  showToast: (message: string, type?: ToastType) => void;
}

// ── Context ───────────────────────────────────────────────────────────────

const ToastContext = createContext<ToastContextValue | null>(null);

let nextId = 0;
const DISMISS_MS = 4000;
const FADE_MS = 300;

// ── Provider ──────────────────────────────────────────────────────────────

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);
  const timers = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map());

  const remove = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
    timers.current.delete(id);
  }, []);

  const startDying = useCallback(
    (id: string) => {
      setToasts((prev) =>
        prev.map((t) => (t.id === id ? { ...t, dying: true } : t)),
      );
      const t = setTimeout(() => remove(id), FADE_MS);
      timers.current.set(id, t);
    },
    [remove],
  );

  const showToast = useCallback(
    (message: string, type: ToastType = "info") => {
      const id = String(++nextId);
      setToasts((prev) => [...prev, { id, message, type, dying: false }]);
      const t = setTimeout(() => startDying(id), DISMISS_MS);
      timers.current.set(id, t);
    },
    [startDying],
  );

  // Clear all timers on unmount.
  useEffect(() => {
    const map = timers.current;
    return () => map.forEach((t) => clearTimeout(t));
  }, []);

  return (
    <ToastContext.Provider value={{ showToast }}>
      {children}
      <ToastStack toasts={toasts} onClose={startDying} />
    </ToastContext.Provider>
  );
}

// ── Hook ──────────────────────────────────────────────────────────────────

export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) throw new Error("useToast must be used inside ToastProvider");
  return ctx;
}

// ── Display ───────────────────────────────────────────────────────────────

const ICON: Record<ToastType, string> = {
  success: "✓",
  error: "✕",
  info: "i",
};

function ToastStack({
  toasts,
  onClose,
}: {
  toasts: Toast[];
  onClose: (id: string) => void;
}) {
  if (toasts.length === 0) return null;
  return (
    <div style={styles.stack}>
      {toasts.map((t) => (
        <div
          key={t.id}
          style={{
            ...styles.toast,
            ...(t.dying ? styles.toastOut : styles.toastIn),
            borderLeftColor:
              t.type === "success"
                ? "var(--green)"
                : t.type === "error"
                  ? "var(--red)"
                  : "var(--accent)",
          }}
        >
          <span
            style={{
              ...styles.icon,
              color:
                t.type === "success"
                  ? "var(--green)"
                  : t.type === "error"
                    ? "var(--red)"
                    : "var(--accent)",
            }}
          >
            {ICON[t.type]}
          </span>
          <span style={styles.message}>{t.message}</span>
          <button style={styles.close} onClick={() => onClose(t.id)}>
            ✕
          </button>
        </div>
      ))}
    </div>
  );
}

// ── Inline styles (no class names needed for a tiny fixed element) ────────

const styles = {
  stack: {
    position: "fixed" as const,
    bottom: 20,
    right: 20,
    zIndex: 9999,
    display: "flex",
    flexDirection: "column" as const,
    gap: 8,
    pointerEvents: "none" as const,
  },
  toast: {
    display: "flex",
    alignItems: "center",
    gap: 10,
    padding: "10px 14px",
    background: "var(--bg-card)",
    border: "1px solid var(--border)",
    borderLeft: "3px solid var(--accent)",
    borderRadius: "var(--radius)",
    boxShadow: "0 4px 20px rgba(0,0,0,0.4)",
    maxWidth: 360,
    pointerEvents: "auto" as const,
  },
  toastIn: {
    animation: "toast-in 200ms ease forwards",
  },
  toastOut: {
    animation: "toast-out 300ms ease forwards",
  },
  icon: {
    fontWeight: 700,
    fontSize: 12,
    flexShrink: 0,
    width: 16,
    textAlign: "center" as const,
  },
  message: {
    flex: 1,
    fontSize: 12,
    color: "var(--text-primary)",
    wordBreak: "break-word" as const,
  },
  close: {
    background: "none",
    color: "var(--text-muted)",
    fontSize: 10,
    padding: "2px 4px",
    flexShrink: 0,
  },
} as const;
