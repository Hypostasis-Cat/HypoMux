// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ProxyChainDialog } from "./ProxyChainDialog";
import { LanguageProvider } from "../i18n/i18n";

const renderDialog = (overrides: Partial<Parameters<typeof ProxyChainDialog>[0]> = {}) => {
  const props = {
    open: true,
    socksPort: 12800,
    systemProxyTakeover: true,
    busy: false,
    onClose: vi.fn(),
    onEnableChainMode: vi.fn().mockResolvedValue(true),
    ...overrides,
  };
  render(<LanguageProvider><ProxyChainDialog {...props} /></LanguageProvider>);
  return props;
};

describe("ProxyChainDialog", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("releases the system proxy and copies the script for the configured port", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } });
    // FluentUI MessageBar reflows through ResizeObserver, which jsdom lacks.
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
    const props = renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "关闭系统代理接管" }));
    expect(props.onEnableChainMode).toHaveBeenCalledTimes(1);

    const code = screen.getByLabelText("联动配置") as HTMLTextAreaElement;
    expect(code.value).toContain("const HYPOMUX_PORT = 12800;");
    fireEvent.click(screen.getByRole("button", { name: "复制配置" }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(code.value));
    expect(await screen.findByText("已复制到剪贴板。")).toBeTruthy();
  });

  it("offers the YAML snippet and hides the takeover action once released", () => {
    renderDialog({ systemProxyTakeover: false });

    expect(screen.queryByRole("button", { name: "关闭系统代理接管" })).toBeNull();
    fireEvent.click(screen.getByRole("tab", { name: "Mihomo YAML" }));
    expect((screen.getByLabelText("联动配置") as HTMLTextAreaElement).value).toContain("port: 12800");
  });
});
