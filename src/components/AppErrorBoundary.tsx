import { Component, type ErrorInfo, type ReactNode } from "react";

// A dynamic import fails like this when a deploy removed the old page chunk.
const isChunkLoadError = (error: unknown) =>
  error instanceof Error &&
  /dynamically imported module|Importing a module script failed|error loading dynamically imported module|ChunkLoadError/i.test(
    `${error.name} ${error.message}`,
  );

// Last-resort screen for render errors, so users get a way back instead of a
// blank page. Errors are still reported as uncaught so monitoring and the
// browser test suites (which watch "pageerror") keep seeing real crashes.
export default class AppErrorBoundary extends Component<
  { children: ReactNode },
  { error: unknown }
> {
  state = { error: null as unknown };
  static getDerivedStateFromError(error: unknown) {
    return { error };
  }
  componentDidCatch(error: unknown, _info: ErrorInfo) {
    if (isChunkLoadError(error)) return;
    if (typeof window.reportError === "function") window.reportError(error);
    else
      setTimeout(() => {
        throw error;
      });
  }
  render() {
    if (!this.state.error) return this.props.children;
    const deploy = isChunkLoadError(this.state.error);
    return (
      <div className="app-error-screen" role="alert">
        <h1>{deploy ? "页面需要更新" : "页面出错了"}</h1>
        <p>
          {deploy
            ? "网站刚刚更新，请刷新页面以加载新版本。"
            : "请刷新页面重试；如果问题持续出现，请提交工单并说明操作步骤。"}
        </p>
        <button
          type="button"
          className="button primary"
          onClick={() => window.location.reload()}
        >
          刷新页面
        </button>
      </div>
    );
  }
}
