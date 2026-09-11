// @vitest-environment jsdom
import { render, screen, fireEvent, waitFor, cleanup } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SteamCDNPanel } from "./SteamCDNPanel";
const mocks = vi.hoisted(() => ({ status: vi.fn() }));
vi.mock("../platform/services", () => ({ appServices: { engine: { steamCDNStatus: mocks.status } } }));
vi.mock("../i18n/i18n", () => ({ useI18n: () => ({ locale: "en" }) }));
afterEach(cleanup);
beforeEach(() => { vi.clearAllMocks(); mocks.status.mockReset(); });
describe("Steam diagnostics", () => {
  it("keeps short-test references separate from actual transfer and displays the budget", async () => {
    mocks.status.mockResolvedValue({ available: true, enabled: true, recognized: 8, probing: 0, replacements: 0, fallbacks: 0, speed_probe_bytes: 524288, speed_probe_limit: 4194304, stage_counts: { http_speed_sampled: 1 }, entries: [
      { adapter: "Ethernet", domain: "cache1.steamcontent.com", port: "80", ip: "5.6.7.8", source: "dns", validated: false, preferred: false, decision_reason: "content_mismatch", probe_bps: 10485760, probed_at: "2020-01-01T00:00:00Z", active_connections: 0, cooldown_until: "0001-01-01T00:00:00Z", expires_at: "2099-01-01T00:00:00Z" },
    ] });
    render(<SteamCDNPanel enabled saving={false} />);
    expect(await screen.findByText(/Short tests: 0.50 \/ 4 MiB/)).toBeTruthy();
    expect(screen.getByText("Candidate unavailable")).toBeTruthy();
    expect(screen.getByText("Content check failed; candidate withdrawn")).toBeTruthy();
    expect(screen.getByLabelText("Short-test references").textContent).toContain("not actual download rate · expired");
    expect(screen.getByText("Idle")).toBeTruthy();
  });
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
  it("distinguishes original observation from a verified candidate and shows totals", async () => {
    mocks.status.mockResolvedValue({ available: true, enabled: true, recognized: 8, probing: 0, replacements: 1, effective_replacements: 1, fallbacks: 0, stage_counts: { http_signed_eligible: 4, verified: 1 }, entries: [
      { adapter: "Ethernet", domain: "xz.sycontroller.com", port: "80", ip: "1.2.3.4", validated: false, preferred: false, active_connections: 2, total_bps: 20971520, download_bps: 1024, samples: 99, cooldown_until: "0001-01-01T00:00:00Z", expires_at: "2099-01-01T00:00:00Z" },
      { adapter: "Ethernet", domain: "xz.sycontroller.com", port: "80", ip: "5.6.7.8", decision_reason: "baseline_missing", validated: true, preferred: false, active_connections: 0, total_bps: 0, samples: 0, cooldown_until: "0001-01-01T00:00:00Z", expires_at: "2099-01-01T00:00:00Z" },
    ] });
    render(<SteamCDNPanel enabled saving={false} />);
    expect(await screen.findByText("Original node · observation only")).toBeTruthy();
    expect(screen.getByText("Verified candidate")).toBeTruthy();
    expect(screen.getByText("Waiting for a fresh original-node baseline")).toBeTruthy();
    expect(screen.getByText("20.00 MiB/s")).toBeTruthy();
    expect(screen.getByText(/1 connections switched · 1 transferred data/)).toBeTruthy();
    expect(screen.getByText("Eligible chunk requests: 4 · Candidate validations passed: 1")).toBeTruthy();
  });



});
