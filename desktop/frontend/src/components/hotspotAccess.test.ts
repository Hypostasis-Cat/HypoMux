import { expect, it } from "vitest";
import { createHotspotPassword, hotspotQRPayload } from "./hotspotAccess";

it("escapes Wi-Fi payload delimiters while preserving Unicode names", () => {
  expect(hotspotQRPayload({ ssid: '校园;网:1,\\"', password: 'pass;P:other', band: "auto" }))
    .toBe('WIFI:T:WPA;S:校园\\;网\\:1\\,\\\\\\";P:pass\\;P\\:other;;');
});
it("creates distinct valid initial passwords", () => {
  const first = createHotspotPassword();
  expect(first).toMatch(/^[A-Za-z0-9_-]{16}$/);
  expect(createHotspotPassword()).not.toBe(first);
});
