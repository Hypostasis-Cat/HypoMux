import { Call } from "@wailsio/runtime";
import { isDesktopRuntime } from "./runtime";

export interface AIConfig { protocol: string; base_url: string; model: string; has_key: boolean }
export interface AIEntry { id: string; role: string; text: string; tool?: string; arguments?: string; state?: string; source?: string; at: string }
export interface AISnapshot { running: boolean; entries: AIEntry[]; error?: string; revision: number; pending: number }
export interface MCPStatus { enabled: boolean; url: string; read_only: boolean }
export interface MCPConnection { status: MCPStatus; token: string }
function call<T>(method: string, ...args: unknown[]): Promise<T> {
  if (!isDesktopRuntime()) return Promise.reject(new Error("请在 HypoMux 桌面程序中使用 AI；浏览器预览不会调用模型或修改网络。"));
  return Call.ByName(`github.com/Hypostasis-Cat/HypoMux/desktop/internal/services.AIService.${method}`, ...args) as Promise<T>;
}
export const aiService = {
  models: (config: AIConfig, key: string, clear: boolean) => call<Array<{ id: string; name: string }>>("ListModels", config, key, clear),
  config: () => call<AIConfig>("Config"),
  saveConfig: (config: AIConfig, key: string, clear: boolean) => call<AIConfig>("SaveConfig", config, key, clear),
  snapshot: () => call<AISnapshot>("Snapshot"),
  send: (text: string, context?: string) => context ? call<void>("SendWithContext", text, context) : call<void>("Send", text),
  cancel: () => call<void>("Cancel"),
  decide: (id: string, allow: boolean) => call<void>("Decide", id, allow),
  clear: () => call<void>("ClearHistory"),
  test: () => call<string>("TestConnection"),
  mcpStatus: () => call<MCPStatus>("MCPStatus"),
  enableMCP: (port: number, readOnly: boolean) => call<MCPConnection>("EnableMCP", port, readOnly),
  disableMCP: () => call<void>("DisableMCP"),
};
