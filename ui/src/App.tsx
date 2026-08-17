import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { AuthProvider, useAuth } from "./contexts/AuthContext.tsx";
import { ToastProvider } from "./contexts/ToastContext.tsx";
import { BalanceProvider } from "./contexts/BalanceContext.tsx";
import { LoginPage } from "./pages/LoginPage.tsx";
import { TradingPage } from "./pages/TradingPage.tsx";
import { HistoryPage } from "./pages/HistoryPage.tsx";
import { FaucetPage } from "./pages/FaucetPage.tsx";

function AppShell() {
  const { client } = useAuth();
  // Show trading view as soon as a client exists (even unauthenticated / guest).
  if (!client) return <LoginPage />;

  return (
    <Routes>
      <Route path="/" element={<TradingPage />} />
      <Route path="/history" element={<HistoryPage />} />
      <Route path="/faucet" element={<FaucetPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

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
