import { useCallback, useEffect, useRef } from "react";
import { apiPost } from "../../lib/api";
import { consoleSessionAttachUrl, limitTranscript, parseConsoleSocketMessage } from "../app-shell-runtime";

export function useConsoleConnections({ setConsoleSessions }) {
  const connectionsRef = useRef({});
  const expectedClosuresRef = useRef(new WeakSet());

  const closeExpected = useCallback((connection) => {
    if (!connection) return;
    expectedClosuresRef.current.add(connection);
    connection.close();
  }, []);

  const patchSession = useCallback(
    (sessionID, updater) => {
      setConsoleSessions((current) => {
        const index = current.data.findIndex((session) => Number(session.id) === Number(sessionID));
        if (index === -1) return current;
        const data = [...current.data];
        data[index] = { ...data[index], ...updater(data[index]) };
        return { state: "ready", data, error: null };
      });
    },
    [setConsoleSessions],
  );

  const disconnectAll = useCallback(() => {
    Object.values(connectionsRef.current).forEach(closeExpected);
    connectionsRef.current = {};
  }, [closeExpected]);

  const disconnectSessions = useCallback(
    (sessionIDs) => {
      for (const sessionID of sessionIDs) {
        const connection = connectionsRef.current[sessionID];
        if (!connection) continue;
        closeExpected(connection);
        delete connectionsRef.current[sessionID];
      }
    },
    [closeExpected],
  );

  const attachSession = useCallback(
    (sessionID, options = {}) => {
      const existing = connectionsRef.current[sessionID];
      if (existing && (existing.readyState === WebSocket.OPEN || existing.readyState === WebSocket.CONNECTING)) {
        if (!options.force) return;
        closeExpected(existing);
      }
      if (existing && (existing.readyState === WebSocket.CLOSING || existing.readyState === WebSocket.CLOSED)) {
        delete connectionsRef.current[sessionID];
      }

      patchSession(sessionID, () => ({ status: "connecting", error: null }));
      const socket = new WebSocket(consoleSessionAttachUrl(sessionID));
      connectionsRef.current[sessionID] = socket;

      socket.onopen = () => {
        if (connectionsRef.current[sessionID] !== socket) return;
      };
      socket.onmessage = (event) => {
        if (connectionsRef.current[sessionID] !== socket) return;
        const message = parseConsoleSocketMessage(event.data);
        if (!message) {
          patchSession(sessionID, (session) => ({
            transcript: limitTranscript(`${session.transcript || ""}\r\n[console protocol warning] Ignored malformed server message.\r\n`),
            status: session.status,
            error: "Ignored malformed console server message.",
          }));
          return;
        }
        if (message.type === "snapshot") {
          patchSession(sessionID, () => ({
            transcript: message.data || "",
            status: message.status || "connected",
            error: null,
          }));
        }
        if (message.type === "ready") {
          patchSession(sessionID, () => ({ status: "connected", error: null }));
        }
        if (message.type === "output") {
          patchSession(sessionID, (session) => ({
            transcript: limitTranscript(`${session.transcript || ""}${message.data || ""}`),
            status: message.status || "connected",
            error: null,
          }));
        }
        if (message.type === "error") {
          patchSession(sessionID, (session) => ({
            transcript: limitTranscript(`${session.transcript || ""}\r\n${message.data || "PTY error"}\r\n`),
            status: "error",
            error: message.data || "PTY error",
          }));
        }
        if (message.type === "exit") {
          expectedClosuresRef.current.add(socket);
          patchSession(sessionID, (session) => ({
            transcript: limitTranscript(`${session.transcript || ""}\r\n[session closed]\r\n`),
            status: message.status || "closed",
            error: message.data || "",
          }));
          closeExpected(socket);
          if (connectionsRef.current[sessionID] === socket) delete connectionsRef.current[sessionID];
        }
      };
      socket.onerror = () => {
        if (connectionsRef.current[sessionID] !== socket) return;
        patchSession(sessionID, () => ({ status: "error", error: "PTY connection failed." }));
      };
      socket.onclose = () => {
        if (connectionsRef.current[sessionID] !== socket) return;
        delete connectionsRef.current[sessionID];
        if (expectedClosuresRef.current.has(socket)) return;
        patchSession(sessionID, (session) => ({
          status: "error",
          error: session.error || "Console connection closed unexpectedly. Reconnect to continue.",
        }));
      };
    },
    [closeExpected, patchSession],
  );

  const sendInput = useCallback(
    (sessionID, data) => {
      const socket = connectionsRef.current[sessionID];
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "input", data }));
        return;
      }
      attachSession(sessionID);
      void apiPost(`/api/console/sessions/${sessionID}/input`, { data }).catch((error) => {
        patchSession(sessionID, () => ({ status: "error", error: error.message }));
      });
    },
    [attachSession, patchSession],
  );

  const resizeSession = useCallback((sessionID, cols, rows) => {
    const socket = connectionsRef.current[sessionID];
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "resize", cols, rows }));
    }
  }, []);

  const closeSession = useCallback(
    async (sessionID) => {
      const connection = connectionsRef.current[sessionID];
      if (connection) expectedClosuresRef.current.add(connection);
      try {
        await apiPost(`/api/console/sessions/${sessionID}/close`, {});
      } catch (error) {
        if (connection && connectionsRef.current[sessionID] === connection) expectedClosuresRef.current.delete(connection);
        throw error;
      }
      if (connectionsRef.current[sessionID] === connection) {
        closeExpected(connection);
        delete connectionsRef.current[sessionID];
      }
      patchSession(sessionID, () => ({ status: "closed" }));
    },
    [closeExpected, patchSession],
  );

  useEffect(() => disconnectAll, [disconnectAll]);

  return {
    attachSession,
    closeSession,
    disconnectAll,
    disconnectSessions,
    resizeSession,
    sendInput,
  };
}
