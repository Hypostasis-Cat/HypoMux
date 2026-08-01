import { System } from "@wailsio/runtime";

type WailsBridgeWindow = Window & {
  chrome?: { webview?: { postMessage?: unknown } };
  webkit?: { messageHandlers?: { external?: { postMessage?: unknown } } };
  wails?: { invoke?: unknown };
};

const hasNativeWailsBridge = () => {
  const runtimeWindow = window as WailsBridgeWindow;
  return Boolean(
    runtimeWindow.chrome?.webview?.postMessage ||
    runtimeWindow.webkit?.messageHandlers?.external?.postMessage ||
    runtimeWindow.wails?.invoke,
  );
};

// System.IsDesktop() depends on window._wails.environment, which Wails injects
// shortly after the document starts. The native bridge is available earlier;
// checking both prevents the first appearance load from being mistaken for a
// browser preview and the defaults from being written over persisted settings.
export const isDesktopRuntime = () => System.IsDesktop() || hasNativeWailsBridge();
