import { AuthProvider, useAuth } from "./contexts/AuthContext.tsx";
import { ToastProvider } from "./contexts/ToastContext.tsx";
import { LoginPage } from "./pages/LoginPage.tsx";
import { TradingPage } from "./pages/TradingPage.tsx";

function AppShell() {
  const { client } = useAuth();
  // Show trading view as soon as a client exists (even unauthenticated / guest).
  return client ? <TradingPage /> : <LoginPage />;
}

export default function App() {
  return (
    <ToastProvider>
      <AuthProvider>
        <AppShell />
      </AuthProvider>
    </ToastProvider>
  );
}
