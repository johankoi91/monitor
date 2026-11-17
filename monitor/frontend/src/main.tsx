import React from "react";
import { createRoot } from "react-dom/client";
import ConfigProvider from "antd/es/config-provider";
import zhCN from "antd/es/locale/zh_CN";
import App from "./App";
import "antd/dist/reset.css";
import "./style.css";
import { PageErrorBoundary } from "./PageErrorBoundary";
const nonce = document.querySelector<HTMLMetaElement>(
  'meta[name="csp-nonce"]',
)?.content;
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <PageErrorBoundary>
      <ConfigProvider
        csp={nonce && !nonce.startsWith("__AVOPS_") ? { nonce } : undefined}
        locale={zhCN}
        button={{ autoInsertSpace: false }}
        theme={{
          token: {
            colorPrimary: "#1677ff",
            colorInfo: "#1677ff",
            borderRadius: 8,
            fontSizeHeading2: 24,
            fontSizeHeading3: 18,
            fontSizeHeading4: 16,
            fontFamily: 'system-ui, -apple-system, "PingFang SC", sans-serif',
          },
        }}
      >
        <App />
      </ConfigProvider>
    </PageErrorBoundary>
  </React.StrictMode>,
);
