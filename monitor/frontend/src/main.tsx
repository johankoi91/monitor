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
            colorLink: "#0958d9",
            colorText: "#1f2937",
            colorTextSecondary: "#475569",
            colorTextTertiary: "#526174",
            colorSuccess: "#166534",
            colorSuccessBg: "#eefaf2",
            colorSuccessBorder: "#c4e2ce",
            colorWarning: "#92400e",
            colorWarningBg: "#fff8e8",
            colorWarningBorder: "#edd7a8",
            colorError: "#b42318",
            colorErrorBg: "#fff1f0",
            colorErrorBorder: "#f0c8c2",
            colorBgLayout: "#f5f7fa",
            colorBorder: "#dce3eb",
            motion: false,
            borderRadius: 8,
            fontSizeHeading2: 22,
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
