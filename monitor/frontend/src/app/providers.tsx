import ConfigProvider, {
  type ConfigProviderProps,
} from "antd/es/config-provider";
import { unstableSetRender } from "antd/es/config-provider/UnstableContext";
import zhCN from "antd/es/locale/zh_CN";
import { type ReactNode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { PageErrorBoundary } from "../shared/ui/PageErrorBoundary";

const nonce = document.querySelector<HTMLMetaElement>(
  'meta[name="csp-nonce"]',
)?.content;
const uiConfig: ConfigProviderProps = {
  csp: nonce && !nonce.startsWith("__AVOPS_") ? { nonce } : undefined,
  locale: zhCN,
  button: { autoInsertSpace: false },
  theme: {
    token: {
      colorPrimary: "#1677ff",
      colorInfo: "#1677ff",
      borderRadius: 8,
      fontSizeHeading2: 24,
      fontSizeHeading3: 18,
      fontSizeHeading4: 16,
      fontFamily: 'system-ui, -apple-system, "PingFang SC", sans-serif',
    },
  },
};

// Ant Design 5 static dialogs need the React 19 renderer and the page's CSP nonce.
const dialogRoots = new WeakMap<Element | DocumentFragment, Root>();
unstableSetRender((node, container) => {
  let root = dialogRoots.get(container);
  if (!root) {
    root = createRoot(container);
    dialogRoots.set(container, root);
  }
  root.render(node);
  return async () => {
    await new Promise<void>((resolve) => setTimeout(resolve, 0));
    root.unmount();
    dialogRoots.delete(container);
  };
});
ConfigProvider.config({
  holderRender: (children) => (
    <ConfigProvider {...uiConfig}>{children}</ConfigProvider>
  ),
});

export default function AppProviders({ children }: { children: ReactNode }) {
  return (
    <PageErrorBoundary>
      <ConfigProvider {...uiConfig}>{children}</ConfigProvider>
    </PageErrorBoundary>
  );
}
