import { Component, type ReactNode } from "react";

// Keep a visible recovery path even if a component fails before Ant Design
// can render. Do not expose API response bodies or credentials in the error.
export class PageErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <main role="alert">
        <h1>页面暂时无法显示</h1>
        <p>请重新加载页面。如果问题持续，请联系运维人员。</p>
        <button type="button" onClick={() => window.location.reload()}>重新加载</button>
      </main>
    );
  }
}
