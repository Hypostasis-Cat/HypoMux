import { Call } from "@wailsio/runtime";
import { isDesktopRuntime } from "./runtime";

export async function saveCompanionFile(name: string, bytes: Uint8Array): Promise<"saved" | "cancelled" | "download"> {
  if (isDesktopRuntime()) {
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += 8192) binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192));
    const saved = await Call.ByName("github.com/Hypostasis-Cat/HypoMux/desktop/internal/platform/wails.DesktopHost.ExportCompanionFile", name, btoa(binary)) as boolean;
    return saved ? "saved" : "cancelled";
  }
  const url = URL.createObjectURL(new Blob([new Uint8Array(bytes)], { type: name.endsWith(".md") ? "text/markdown;charset=utf-8" : "application/zip" }));
  const link = document.createElement("a"); link.href = url; link.download = name;
  document.body.append(link); link.click(); link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
  return "download";
}
