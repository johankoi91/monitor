import React from "react";
import { createRoot } from "react-dom/client";
import App from "./app/App";
import AppProviders from "./app/providers";
import "./app/styles";

createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <AppProviders>
      <App />
    </AppProviders>
  </React.StrictMode>,
);
