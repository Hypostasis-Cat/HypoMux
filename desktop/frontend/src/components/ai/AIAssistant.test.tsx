// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AIAssistant } from "./AIAssistant";

const mocks = vi.hoisted(() => ({
  models: vi.fn(), config: vi.fn(), snapshot: vi.fn(), mcpStatus: vi.fn(), send: vi.fn(), cancel: vi.fn(), decide: vi.fn(), clear: vi.fn(), saveConfig: vi.fn(), test: vi.fn(), enableMCP: vi.fn(), disableMCP: vi.fn(),
}));
vi.mock("../../platform/ai", () => ({ aiService: mocks }));
vi.mock("../../platform/runtime", () => ({ isDesktopRuntime: () => true }));
vi.mock("../../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.config.mockResolvedValue({ protocol: "openai", base_url: "https://example.com/v1", model: "test-model", has_key: true });
  mocks.snapshot.mockResolvedValue({ running: false, entries: [], revision: 0, pending: 0 });
  mocks.mcpStatus.mockResolvedValue({ enabled: false, url: "", read_only: true });
  mocks.send.mockResolvedValue(undefined); mocks.decide.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe("AI assistant", () => {
  it("fetches models from the form without saving and allows selection", async () => {
    mocks.models.mockResolvedValue([{ id: "test-model", name: "Test model" }, { id: "another-model", name: "Another model" }]);
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await screen.findByText("test-model");
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    await screen.findByText("Loaded 2 models. Search and select above.");
    expect(mocks.models).toHaveBeenCalledWith(expect.objectContaining({ base_url: "https://example.com/v1" }), "", false);
    expect(mocks.saveConfig).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("combobox", { name: "Model ID" }));
    fireEvent.click(await screen.findByRole("option", { name: /Another model/ }));
    expect((screen.getByRole("combobox", { name: "Model ID" }) as HTMLInputElement).value).toBe("another-model");
  });
  it("speaks in a standalone bubble while the input is collapsed", async () => {
    mocks.snapshot.mockResolvedValue({ running: false, revision: 0, pending: 0, entries: [{ id: "reply", role: "assistant", text: "I can help check this network.", at: "" }] });
    render(<AIAssistant open={false} workspace={false} onOpenChange={vi.fn()} />);
    await screen.findByText("I can help check this network.");
    expect(screen.queryByRole("textbox", { name: "Message" })).toBeNull();
    expect(screen.getByRole("button", { name: "Network companion" }).getAttribute("aria-expanded")).toBe("false");
  });
  it("tracks page changes and sends a frozen context with selected rules", async () => {
    const onOpenChange = vi.fn();
    const { rerender } = render(<AIAssistant open workspace={false} page="home" onOpenChange={onOpenChange} />);
    await screen.findByText("test-model");
    rerender(<AIAssistant open workspace={false} page="routing" onOpenChange={onOpenChange} />);
    expect(screen.getByText("Viewing: Routing rules")).toBeTruthy();
    window.dispatchEvent(new CustomEvent("hypomux:ai-selection", { detail: { page: "routing", selection: '[{"value":"cs2.exe"}]' } }));
    await screen.findByText("Rules selected");
    fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "make this direct" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.send).toHaveBeenCalled());
    const captured = mocks.send.mock.calls[0][1];
    expect(JSON.parse(captured)).toMatchObject({ page: "routing", selected_rules: '[{"value":"cs2.exe"}]' });
    rerender(<AIAssistant open workspace={false} page="health" onOpenChange={onOpenChange} />);
    expect(screen.getByText("Viewing: Network health")).toBeTruthy();
    expect(screen.queryByText("Rules selected")).toBeNull();
    expect(JSON.parse(captured).page).toBe("routing");
  });
  it("preserves drafts when promoting quick chat and navigating away without cancelling work", async () => {
    const onOpenChange = vi.fn();
    const onOpenWorkspace = vi.fn();
    const { rerender } = render(<AIAssistant open workspace={false} onOpenChange={onOpenChange} onOpenWorkspace={onOpenWorkspace} />);
    await screen.findByText("test-model");
    fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "route CS2 directly" } });
    fireEvent.click(screen.getByRole("button", { name: "Open workspace" }));
    expect(onOpenWorkspace).toHaveBeenCalledOnce();
    rerender(<AIAssistant open workspace onOpenChange={onOpenChange} />);
    expect((screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement).value).toBe("route CS2 directly");
    rerender(<AIAssistant open={false} workspace={false} onOpenChange={onOpenChange} />);
    rerender(<AIAssistant open workspace onOpenChange={onOpenChange} />);
    expect((screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement).value).toBe("route CS2 directly");
    expect(mocks.cancel).not.toHaveBeenCalled();
  });
  it("sends user intent to the backend without executing guessed frontend operations", async () => {
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await screen.findByText("test-model");
    fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "Start aggregation; route CS2 directly" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(mocks.send).toHaveBeenCalledWith("Start aggregation; route CS2 directly", expect.stringContaining('"page":"home"')));
    expect(mocks.decide).not.toHaveBeenCalled();
  });
  it("requires an explicit decision bound to the pending operation ID", async () => {
    mocks.snapshot.mockResolvedValue({ running: true, revision: 0, pending: 1, entries: [{ id: "specific-id", role: "tool", tool: "set_rule", arguments: '{"value":"cs2.exe","outbound":"direct"}', state: "waiting", source: "mcp", text: "", at: "" }] });
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await screen.findByRole("button", { name: "Allow once" });
    expect(mocks.decide).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Deny" }));
    await waitFor(() => expect(mocks.decide).toHaveBeenCalledWith("specific-id", false));
  });
  it("keeps credentials out of chat and tests only after saving configuration", async () => {
    mocks.saveConfig.mockResolvedValue({ protocol: "openai", base_url: "https://example.com/v1", model: "test-model", has_key: true });
    mocks.test.mockResolvedValue("Tool calling verified");
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await screen.findByText("test-model");
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    fireEvent.change(screen.getByLabelText("API Key"), { target: { value: "private-key" } });
    fireEvent.click(screen.getByRole("button", { name: "Save and test tool calling" }));
    await screen.findByText("Tool calling verified");
    expect(mocks.saveConfig).toHaveBeenCalledWith(expect.objectContaining({ model: "test-model" }), "private-key", false);
    expect(mocks.send).not.toHaveBeenCalled();
    expect((screen.getByLabelText("API Key") as HTMLInputElement).value).toBe("");
  });
  it("preserves user input and exposes backend failures", async () => {
    mocks.send.mockRejectedValue(new Error("Model is not configured"));
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await screen.findByText("test-model");
    fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "check network" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByRole("alert");
    expect((screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement).value).toBe("check network");
  });
});
