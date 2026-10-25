import React from "react";
import ReactDOM from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ToastProvider, ConfirmProvider } from "./lib";
import App from "./App";
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
import { preferredTheme } from "./theme";
document.documentElement.dataset.theme = preferredTheme();
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
            <App />
          </ConfirmProvider>
        </ToastProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
);
