import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "./contexts/AuthContext.tsx";
import { ToastProvider } from "./contexts/ToastContext.tsx";
import { BalanceProvider } from "./contexts/BalanceContext.tsx";
import { LoginPage } from "./pages/LoginPage.tsx";
import { TradingPage } from "./pages/TradingPage.tsx";
import { HistoryPage } from "./pages/HistoryPage.tsx";
import { FaucetPage } from "./pages/FaucetPage.tsx";
import { SessionsPage } from "./pages/SessionsPage.tsx";

function RestoringScreen() {
  return (
    <div style={splash.page}>
      <span style={splash.icon}>⬡</span>
      <span style={splash.text}>Restoring session…</span>
    </div>
  );
}

function AppShell() {
  const { client, restoring } = useAuth();
  // A stored token is being checked against the API — rendering the login form
  // here would flash it away a moment later for anyone already signed in.
  if (restoring) return <RestoringScreen />;
  // Show trading view as soon as a client exists (even unauthenticated / guest).
  if (!client) return <LoginPage />;

  return (
    <Routes>
      <Route path="/" element={<TradingPage />} />
      <Route path="/history" element={<HistoryPage />} />
      <Route path="/faucet" element={<FaucetPage />} />
      <Route path="/sessions" element={<SessionsPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

const splash = {
  page: {
    height: "100%",
    display: "flex",
    flexDirection: "column" as const,
    alignItems: "center",
    justifyContent: "center",
    gap: 10,
    background: "var(--bg-base)",
  },
  icon: {
    fontSize: 32,
    color: "var(--accent)",
    animation: "pulse 1.2s ease-in-out infinite",
  },
  text: {
    fontSize: 12,
    color: "var(--text-muted)",
  },
} as const;

export default function App() {
  return (
    <ToastProvider>
      <AuthProvider>
        <BalanceProvider>
          <BrowserRouter>
            <AppShell />
          </BrowserRouter>
        </BalanceProvider>
      </AuthProvider>
    </ToastProvider>
  );
}
