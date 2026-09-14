import type { HotspotConfig } from "../platform/services";

export function createHotspotPassword() {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789-_";
  let password = "";
  while (password.length < 16) {
    const value = crypto.getRandomValues(new Uint8Array(1))[0];
    if (value < Math.floor(256 / alphabet.length) * alphabet.length) password += alphabet[value % alphabet.length];
  }
  return password;
}

// ZXing Wi-Fi payload. Escape user data so it cannot introduce another field.
export function hotspotQRPayload(config: HotspotConfig) {
  const escape = (value: string) => value.replace(/[\\;,:\"]/g, "\\$&");
  return `WIFI:T:WPA;S:${escape(config.ssid)};P:${escape(config.password)};;`;
}
