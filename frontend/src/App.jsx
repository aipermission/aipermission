import { lazy, Suspense, useEffect, useState } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { apiGet } from "./lib/api";
import { useTheme } from "./lib/theme";
import { Notice } from "./components/ui/notice";
import { Shell } from "./components/app-shell";
import { DashboardPage } from "./pages/dashboard";
import { CredentialsPage } from "./pages/credentials";
import { SettingsPage } from "./pages/settings";
import { SecurityPage } from "./pages/security";
import { HistoryPage } from "./pages/history";
import { AuditLogsPage } from "./pages/audit-logs";
import { MCPSetupPage } from "./pages/mcp-setup";
import { TokensPage } from "./pages/tokens";
import { ConnectorsPage } from "./pages/connectors";
import { UnlockPage, UnlockShell } from "./pages/unlock";

const ConsolePage = lazy(() => import("./pages/console").then((module) => ({ default: module.ConsolePage })));

export default function App() {
  const { theme, setTheme } = useTheme();
  const [unlock, setUnlock] = useState({ state: "loading", data: null, error: null });

  async function loadUnlockStatus() {
    try {
      const data = await apiGet("/api/unlock/status");
      setUnlock({ state: "ready", data, error: null });
    } catch (error) {
      setUnlock({ state: "error", data: null, error: error.message });
    }
  }

  useEffect(() => {
    void loadUnlockStatus();
  }, []);

  useEffect(() => {
    function handleSessionRequired() {
      void loadUnlockStatus();
    }
    window.addEventListener("aipermission:ui-session-required", handleSessionRequired);
    return () => window.removeEventListener("aipermission:ui-session-required", handleSessionRequired);
  }, []);

  if (unlock.state === "loading") {
    return <UnlockShell title="Checking encrypted database..." />;
  }

  if (unlock.state === "error") {
    return (
      <UnlockShell title="Gateway unavailable">
        <Notice tone="bad">{unlock.error}</Notice>
      </UnlockShell>
    );
  }

  if (unlock.data?.state !== "unlocked") {
    return <UnlockPage status={unlock.data} onUnlocked={loadUnlockStatus} />;
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Shell theme={theme} setTheme={setTheme} />}>
          <Route index element={<DashboardPage />} />
          <Route path="/credentials" element={<CredentialsPage />} />
          <Route path="/servers" element={<Navigate to="/connectors" replace />} />
          <Route path="/connectors" element={<ConnectorsPage />} />
          <Route path="/history" element={<HistoryPage />} />
          <Route path="/audit-logs" element={<AuditLogsPage />} />
          <Route path="/tokens" element={<TokensPage />} />
          <Route
            path="/console"
            element={
              <Suspense fallback={<Notice>Loading console...</Notice>}>
                <ConsolePage />
              </Suspense>
            }
          />
          <Route path="/security" element={<SecurityPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          <Route path="/backup" element={<Navigate to="/settings" replace />} />
          <Route path="/mcp-setup" element={<MCPSetupPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
