// @vitest-environment jsdom
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SteamCDNPanel } from "./SteamCDNPanel";
const mocks = vi.hoisted(() => ({ status: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { engine: { steamCDNStatus: mocks.status } } }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
afterEach(cleanup);
beforeEach(() => { mocks.status.mockReset(); });
describe("Steam diagnostics", () => {
  it("shows recognized downloads and a concrete verification failure", async () => {
    mocks.status.mockResolvedValue({ available: true, enabled: true, recognized: 5, probing: 0, replacements: 0, fallbacks: 0, entries: [], diagnostics: [{ domain: "st.dl.eccdnx.com", adapter: "Ethernet", ip: "1.2.3.4", stage: "http_content_mismatch", at: new Date().toISOString() }] });
    render(<SteamCDNPanel enabled saving={false} />);
    expect(await screen.findByText("Recognized downloads: 5")).toBeTruthy();
    expect(screen.getByText(/Candidate content differs from original/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Re-evaluate nodes" }));
    await waitFor(() => expect(mocks.status).toHaveBeenCalledWith(true));
  });
  it("accepts an older core status without diagnostic fields", async () => {
    mocks.status.mockResolvedValue({ available: true, enabled: false, probing: 0, replacements: 0, fallbacks: 0, entries: [] });
    render(<SteamCDNPanel enabled={false} saving={false} />);
    expect(await screen.findByText(/Optimization is inactive/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "Re-evaluate nodes" }).hasAttribute("disabled")).toBe(true);
  });
});
