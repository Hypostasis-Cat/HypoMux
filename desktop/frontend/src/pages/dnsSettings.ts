export const MAX_DNS_SERVERS = 16;

export function validDNSAddress(value: string): boolean {
  value = value.trim();
  if (value.includes(":")) {
    try {
      const address = new URL(`http://[${value}]/`).hostname.slice(1, -1);
      if (address.includes(".")) return false;
      // URL canonicalizes mapped IPv4 addresses to hexadecimal.
      if (address.startsWith("::ffff:")) {
        const parts = address.slice(7).split(":").map(part => parseInt(part, 16));
        if (parts.length !== 2) return false;
        return validDNSAddress([parts[0] >> 8, parts[0] & 255, parts[1] >> 8, parts[1] & 255].join("."));
      }
      return address !== "::" && !/^ff|^fe[89ab]/i.test(address);
    } catch { return false; }
  }
  const parts = value.split(".");
  if (parts.length !== 4 || parts.some(part => !/^(0|[1-9]\d{0,2})$/.test(part) || Number(part) > 255)) return false;
  const numbers = parts.map(Number);
  return numbers.some(part => part !== 0) && numbers[0] < 224 && !(numbers[0] === 169 && numbers[1] === 254);
}

export function validDoHAddress(value: string): boolean {
  value = value.trim();
  try {
    const url = new URL(value);
    if (!value.startsWith("https://") || value.length > 2048 || url.username || url.password || url.hash || !url.hostname) return false;
    const authority = value.slice(8).split(/[/?#]/)[0];
    if (authority.endsWith(":") || authority.includes("@")) return false;
    if (url.port && Number(url.port) < 1) return false;
    // Validate the entered host too: URL otherwise repairs shorthand IPv4,
    // backslashes and Unicode hosts that the desktop parser rejects.
    const host = authority.startsWith("[") ? authority.slice(1, authority.indexOf("]")) : authority.split(":")[0];
    if (host.includes(":") || /^[\d.]+$/.test(host)) return validDNSAddress(host);
    return host.length <= 253 && host.replace(/\.$/, "").split(".").every(label => label.length <= 63 && /^[a-z\d](?:[a-z\d-]*[a-z\d])?$/i.test(label));
  } catch { return false; }
}

export function validDoTAddress(value: string): boolean {
  value = value.trim();
  try {
    const url = new URL(value);
    if (!value.startsWith("tls://") || value.length > 2048 || url.username || url.password || !url.hostname || url.pathname || value.includes("?") || value.includes("#")) return false;
    const authority = value.slice(6);
    if (authority.endsWith(":") || authority.includes("@")) return false;
    if (url.port && Number(url.port) < 1) return false;
    const host = authority.startsWith("[") ? authority.slice(1, authority.indexOf("]")) : authority.split(":")[0];
    if (host.includes(":") || /^[\d.]+$/.test(host)) return validDNSAddress(host);
    return host.length <= 253 && host.replace(/\.$/, "").split(".").every(label => label.length <= 63 && /^[a-z\d](?:[a-z\d-]*[a-z\d])?$/i.test(label));
  } catch { return false; }
}
