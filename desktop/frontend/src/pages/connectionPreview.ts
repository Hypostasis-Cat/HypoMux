import type { ConnectionListSnapshot, ConnectionView } from "../platform/services";

// Explicit development fixture; never used by the desktop engine.
export const createConnectionPreview = (): ConnectionListSnapshot => {
  const now = Date.now();
  const processes = ["cs2.exe", "msedge.exe", "steam.exe", "Discord.exe"];
  const counts = [8, 4, 2, 1];
  const connections: ConnectionView[] = [];
  processes.forEach((process, index) => {
    for (let n = 0; n < counts[index]; n++) {
      const id = connections.length + 1;
      const ip = `203.0.113.${id + 10}`;
      const domain = index === 1 ? ["www.example.com", "static.example.com", "video.example.com", "api.example.com"][n] : undefined;
      const port = index === 0 ? String(27030 + n) : "443";
      connections.push({ id, process, protocol: index === 0 ? "socks5_udp" : "tcp",
        client: `127.0.0.1:${61227 + id}`, target: `${ip}:${port}`, domain,
        remote_ip: ip, remote_port: port, adapter: n % 3 === 0 ? "Wi-Fi" : "Ethernet",
        outbound: index === 3 ? "direct" : "aggregation",
        started_at: new Date(now - 1000 * (720 - index * 120 - n * 15)).toISOString(),
        bytes_up: 16384 * (n + 1), bytes_down: 524288 * (n + 1) });
    }
  });
  return { phase: "running", mode: "proxy", sampled_at: new Date(now).toISOString(), connections };
};
