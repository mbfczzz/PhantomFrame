import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ToastProvider, ConfirmProvider } from "./lib";
import App from "./App";
import AppErrorBoundary from "./components/AppErrorBoundary";
import "./palette.css";
import "./styles.css";
import "./cinematic.css";
import "./controls.css";
import "./composer.css";
import "./developer.css";
import "./services.css";
import "./layout.css";
import "./finish.css";
import "./workspace.css";
import "./admin.css";
import "./catalog.css";
import "./studio-input.css";
import { preferredTheme } from "./theme";
document.documentElement.dataset.theme = preferredTheme();
// After a deploy, an open tab may request page chunks that no longer exist.
// Reload once to pick up the new build instead of leaving a blank page.
const reloadFlag = "phantomframe-chunk-reload";
window.addEventListener("vite:preloadError", (event) => {
  try {
    if (sessionStorage.getItem(reloadFlag)) return;
    sessionStorage.setItem(reloadFlag, "1");
  } catch {
    return;
  }
  event.preventDefault();
  window.location.reload();
});
window.addEventListener("load", () => {
  try {
    setTimeout(() => sessionStorage.removeItem(reloadFlag), 10000);
  } catch {}
});
const client = new QueryClient({
  defaultOptions: {
    queries: { refetchOnWindowFocus: false, staleTime: 10000, retry: false },
  },
});
ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={client}>
      <BrowserRouter>
        <ToastProvider>
          <ConfirmProvider>
            <AppErrorBoundary>
              <App />
            </AppErrorBoundary>
          </ConfirmProvider>
        </ToastProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
);
