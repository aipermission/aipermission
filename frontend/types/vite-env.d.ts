/// <reference types="vite/client" />

declare global {
  interface Window {
    MonacoEnvironment?: { getWorker: () => Worker };
  }
}

export {};
