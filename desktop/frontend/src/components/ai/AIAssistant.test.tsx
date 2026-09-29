// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); });

describe("AI assistant", () => {
  it("keeps an unconfigured form editable while fetching and ignores outdated results", async () => {
    mocks.config.mockResolvedValue({ protocol: "openai", base_url: "https://api.openai.com/v1", model: "", has_key: false });
    let resolve!: (models: Array<{ id: string; name: string }>) => void;
    mocks.models.mockReturnValueOnce(new Promise(done => { resolve = done; }));
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await waitFor(() => expect(mocks.config).toHaveBeenCalled());
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    expect((screen.getByRole("button", { name: "Fetching models…" }) as HTMLButtonElement).disabled).toBe(true);
    for (const label of ["API Base URL", "API Key", "Model ID", "API protocol", "Authentication"]) {
      expect((screen.getByLabelText(label) as HTMLInputElement).disabled).toBe(false);
    }
    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.change(screen.getByLabelText("API Base URL"), { target: { value: "http://localhost:11434/v1" } });
    mocks.models.mockResolvedValueOnce([{ id: "local-model", name: "Local model" }]);
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    await screen.findByText("Loaded 1 models. Search and select above.");
    await act(async () => { resolve([{ id: "old-model", name: "Old model" }]); });
    fireEvent.click(screen.getByRole("combobox", { name: "Model ID" }));
    expect(await screen.findByRole("option", { name: /Local model/ })).toBeTruthy();
    expect(screen.queryByRole("option", { name: /Old model/ })).toBeNull();
    expect(mocks.saveConfig).not.toHaveBeenCalled();
  });
  it("recovers from a failed model request and allows retry", async () => {
    mocks.models.mockRejectedValueOnce(new Error("Invalid API key"));
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    expect((await screen.findByRole("alert")).textContent).toBe("Invalid API key");
    mocks.models.mockResolvedValueOnce([{ id: "retry-model", name: "Retry model" }]);
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    await screen.findByText("Loaded 1 models. Search and select above.");
    expect(screen.queryByRole("alert")).toBeNull();
  });
  it("times out a stalled model request without disabling the form", async () => {
    mocks.models.mockReturnValueOnce(new Promise(() => {}));
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
    vi.useFakeTimers();
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    await act(async () => { await vi.advanceTimersByTimeAsync(30000); });
    expect(screen.getByRole("alert").textContent).toContain("Fetching models timed out");
    expect((screen.getByRole("button", { name: "Fetch models" }) as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByLabelText("API Key") as HTMLInputElement).disabled).toBe(false);
  });
  it.each([0, 425])("opens at the latest message after leaving reading position %s", async position => {
    mocks.snapshot.mockResolvedValue({ running: false, entries: [{ id: "history", role: "user", text: "Previous conversation" }], revision: 1, pending: 0 });
    vi.spyOn(HTMLElement.prototype, "scrollHeight", "get").mockReturnValue(1600);
    const props = { onOpenChange: vi.fn() };
    const { rerender } = render(<AIAssistant {...props} open workspace page="assistant" />);
    await screen.findByText("Previous conversation");
    const conversation = screen.getByRole("tabpanel", { name: "Chat" }).querySelector(".ai-conversation") as HTMLElement;
    conversation.scrollTop = position;
    rerender(<AIAssistant {...props} open workspace={false} page="home" />);
    expect(conversation.isConnected).toBe(false);
    rerender(<AIAssistant {...props} open workspace page="assistant" />);
    const restored = screen.getByRole("tabpanel", { name: "Chat" }).querySelector(".ai-conversation") as HTMLElement;
    expect(restored.scrollTop).toBe(1600);
    rerender(<AIAssistant {...props} open={false} workspace={false} page="settings" />);
    rerender(<AIAssistant {...props} open workspace page="assistant" />);
    expect((screen.getByRole("tabpanel", { name: "Chat" }).querySelector(".ai-conversation") as HTMLElement).scrollTop).toBe(1600);
  });
  it("keeps panel scroll positions and drafts when switching tabs", async () => {
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
    const message = screen.getByRole("textbox", { name: "Message" });
    fireEvent.change(message, { target: { value: "Keep this draft" } });
    const chat = screen.getByRole("tabpanel", { name: "Chat" });
    const conversation = chat.querySelector(".ai-conversation") as HTMLElement;
    conversation.scrollTop = 125;
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    const model = screen.getByRole("tabpanel", { name: "Model" });
    const settings = model.querySelector(".ai-settings") as HTMLElement;
    settings.scrollTop = 240;
    fireEvent.click(screen.getByRole("tab", { name: "External AI" }));
    expect(screen.getAllByRole("tabpanel")).toHaveLength(1);
    fireEvent.click(screen.getByRole("tab", { name: "Chat" }));
    expect(screen.getByRole("tabpanel", { name: "Chat" })).toBe(chat);
    expect(screen.getByRole("textbox", { name: "Message" })).toBe(message);
    expect((message as HTMLTextAreaElement).value).toBe("Keep this draft");
    expect(conversation.scrollTop).toBe(conversation.scrollHeight);
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    expect(screen.getByRole("tabpanel", { name: "Model" })).toBe(model);
    expect(settings.scrollTop).toBe(240);
  });
  it("saves relay authentication and Responses protocol without stale model options", async () => {
    mocks.models.mockResolvedValue([{ id: "stale-model", name: "Stale model" }]);
    mocks.saveConfig.mockImplementation(async config => config);
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    await screen.findByText("Loaded 1 models. Search and select above.");
    fireEvent.change(screen.getByRole("combobox", { name: "API protocol" }), { target: { value: "responses" } });
    fireEvent.change(screen.getByRole("combobox", { name: "Authentication" }), { target: { value: "bearer" } });
    fireEvent.click(screen.getByRole("combobox", { name: "Model ID" }));
    expect(screen.queryByRole("option", { name: /Stale model/ })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mocks.saveConfig).toHaveBeenCalledWith(expect.objectContaining({ protocol: "responses", auth_mode: "bearer" }), "", false));
    await screen.findByText(/Changing service, model or key starts a fresh context/);
  });
  it("fetches models from the form without saving and allows selection", async () => {
    mocks.models.mockResolvedValue([{ id: "test-model", name: "Test model" }, { id: "another-model", name: "Another model" }]);
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
    fireEvent.click(screen.getByRole("tab", { name: "Model" }));
    fireEvent.click(screen.getByRole("button", { name: "Fetch models" }));
    await screen.findByText("Loaded 2 models. Search and select above.");
    expect(mocks.models).toHaveBeenCalledWith(expect.objectContaining({ base_url: "https://example.com/v1" }), "", false);
    expect(mocks.saveConfig).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("combobox", { name: "Model ID" }));
    fireEvent.click(await screen.findByRole("option", { name: /Another model/ }));
    expect((screen.getByRole("combobox", { name: "Model ID" }) as HTMLInputElement).value).toBe("another-model");
  });
  it("hides restored replies and speaks only new replies with a details action", async () => {
    mocks.snapshot.mockResolvedValue({ running: false, revision: 0, pending: 0, entries: [{ id: "reply", role: "assistant", text: "I can help check this network.", at: "" }] });
    render(<AIAssistant open={false} workspace={false} onOpenChange={vi.fn()} />);
    await waitFor(() => expect(mocks.snapshot).toHaveBeenCalled());
    expect(screen.queryByText("I can help check this network.")).toBeNull();
    mocks.snapshot.mockResolvedValue({ running: false, revision: 0, pending: 0, entries: [{ id: "new-reply", role: "assistant", text: "**检查完成。**详细结果在这里。", at: "" }] });
    await screen.findByText("检查完成。", {}, { timeout: 2500 });
    expect(screen.queryByText(/详细结果在这里/)).toBeNull();
    expect(screen.getByRole("button", { name: /View details/ })).toBeTruthy();
    expect(screen.queryByRole("textbox", { name: "Message" })).toBeNull();
    expect(screen.getByRole("button", { name: "Network companion" }).getAttribute("aria-expanded")).toBe("false");
  });
  it("does not replay dismissed replies across pages, workspace remounts or task status changes", async () => {
    vi.useFakeTimers();
    const onOpenChange = vi.fn();
    const { rerender } = render(<AIAssistant open={false} workspace={false} page="home" onOpenChange={onOpenChange} />);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    const reply = (id: string, running = false) => ({ running, revision: 0, pending: 0, entries: [{ id, role: "assistant", text: "Done.", at: "" }] });
    mocks.snapshot.mockResolvedValue(reply("first"));
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.getByText("Done.")).toBeTruthy();
    await act(async () => { await vi.advanceTimersByTimeAsync(6000); });
    expect(screen.queryByText("Done.")).toBeNull();
    rerender(<AIAssistant open workspace page="assistant" onOpenChange={onOpenChange} />);
    expect(screen.getByText("Done.")).toBeTruthy(); // History remains readable.
    rerender(<AIAssistant open={false} workspace={false} page="home" onOpenChange={onOpenChange} />);
    expect(screen.queryByText("Done.")).toBeNull();
    mocks.snapshot.mockResolvedValue(reply("first", true));
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    mocks.snapshot.mockResolvedValue(reply("first"));
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.queryByText("Done.")).toBeNull();
    mocks.snapshot.mockResolvedValue(reply("second"));
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.getByText("Done.")).toBeTruthy(); // Same text, genuinely new reply.
    rerender(<AIAssistant open={false} workspace={false} page="health" onOpenChange={onOpenChange} />);
    expect(screen.queryByText("Done.")).toBeNull();
    rerender(<AIAssistant open={false} workspace={false} page="home" onOpenChange={onOpenChange} />);
    expect(screen.queryByText("Done.")).toBeNull();
    rerender(<AIAssistant open workspace page="assistant" onOpenChange={onOpenChange} />);
    mocks.snapshot.mockResolvedValue(reply("third"));
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.getByText("Done.")).toBeTruthy();
    rerender(<AIAssistant open={false} workspace={false} page="home" onOpenChange={onOpenChange} />);
    expect(screen.queryByText("Done.")).toBeNull();
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
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
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
  it.each([
    { workspace: false, remaining: 0, failure: false, close: true },
    { workspace: false, remaining: 1, failure: false, close: false },
    { workspace: true, remaining: 0, failure: false, close: false },
    { workspace: false, remaining: 0, failure: true, close: false },
  ])("handles approval without hiding errors or other pending actions: %j", async ({ workspace, remaining, failure, close }) => {
    const onOpenChange = vi.fn();
    mocks.snapshot.mockResolvedValue({ running: true, revision: 0, pending: 1, entries: [{ id: "approval", role: "tool", tool: "stop", state: "waiting", text: "", at: "" }] });
    render(<AIAssistant open workspace={workspace} onOpenChange={onOpenChange} />);
    await screen.findByRole("button", { name: "Allow once" });
    mocks.snapshot.mockResolvedValue({ running: true, revision: 0, pending: remaining, entries: [] });
    if (failure) mocks.decide.mockRejectedValueOnce(new Error("Approval failed"));
    fireEvent.click(screen.getByRole("button", { name: "Allow once" }));
    await waitFor(() => expect(mocks.decide).toHaveBeenCalledWith("approval", true));
    if (close) await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    else {
      if (failure) await screen.findByRole("alert");
      else await waitFor(() => expect(screen.queryByRole("button", { name: "Allow once" })).toBeNull());
      expect(onOpenChange).not.toHaveBeenCalled();
    }
  });
  it("keeps credentials out of chat and tests only after saving configuration", async () => {
    mocks.saveConfig.mockResolvedValue({ protocol: "openai", base_url: "https://example.com/v1", model: "test-model", has_key: true });
    mocks.test.mockResolvedValue("Tool calling verified");
    render(<AIAssistant open onOpenChange={vi.fn()} />);
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
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
    await within(screen.getByRole("tabpanel", { name: "Chat" })).findByText("test-model");
    fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "check network" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByRole("alert");
    expect((screen.getByRole("textbox", { name: "Message" }) as HTMLTextAreaElement).value).toBe("check network");
  });
});

it("keeps the companion and its position across ordinary pages and the assistant workspace", async () => {
  const onOpenChange = vi.fn();
  const view = render(<AIAssistant open={false} workspace={false} page="home" onOpenChange={onOpenChange} />);
  const pet = screen.getByRole("button", { name: "Network companion" });
  const companion = pet.closest(".ai-companion") as HTMLElement;
  const character = pet.querySelector("svg");
  fireEvent.keyDown(pet, { key: "ArrowLeft", altKey: true });
  fireEvent.keyDown(pet, { key: "ArrowUp", altKey: true });
  const position = { right: companion.style.right, bottom: companion.style.bottom };
  expect(position).toEqual({ right: "48px", bottom: "52px" });
  view.rerender(<AIAssistant open={false} workspace={false} page="settings" onOpenChange={onOpenChange} />);
  expect(screen.getByRole("button", { name: "Network companion" })).toBe(pet);
  view.rerender(<AIAssistant open workspace page="assistant" onOpenChange={onOpenChange} />);
  expect(companion.isConnected).toBe(true);
  expect(companion.style.display).toBe("none");
  expect(screen.queryByRole("button", { name: "Network companion" })).toBeNull();
  view.rerender(<AIAssistant open={false} workspace={false} page="home" onOpenChange={onOpenChange} />);
  expect(screen.getByRole("button", { name: "Network companion" })).toBe(pet);
  expect(pet.querySelector("svg")).toBe(character);
  expect({ right: companion.style.right, bottom: companion.style.bottom }).toEqual(position);
  await act(async () => {});
});
