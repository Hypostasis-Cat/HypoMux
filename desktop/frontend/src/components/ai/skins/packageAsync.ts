import type { Skin } from "./package";

// Each job owns its worker so failures cannot stall other operations or retain assets.
function run<T>(task: { type: "parse"; bytes: Uint8Array } | { type: "export"; skin: Skin }): Promise<T> {
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL("./package.worker.ts", import.meta.url), { type: "module" });
    const timer = setTimeout(() => { worker.terminate(); reject(new Error("Skin processing timed out / 皮肤处理超时")); }, 60000);
    const finish = () => { clearTimeout(timer); worker.terminate(); };
    worker.onmessage = event => { finish(); event.data.error ? reject(new Error(event.data.error)) : resolve(event.data.result); };
    worker.onerror = () => { finish(); reject(new Error("Skin worker failed / 后台皮肤处理失败，请重试")); };
    worker.onmessageerror = () => { finish(); reject(new Error("Skin worker response failed / 无法读取皮肤处理结果")); };
    try { worker.postMessage(task); } catch (error) { finish(); reject(error); }
  });
}
export async function parseSkinAsync(bytes: Uint8Array): Promise<Skin> {
  // DOM-only test environments do not provide Workers. Production never retries a
  // failed worker on the UI thread, which would reintroduce the freeze.
  if (typeof Worker === "undefined") return (await import("./package")).parseSkin(bytes);
  return run<Skin>({ type: "parse", bytes });
}
export async function exportSkinAsync(skin: Skin): Promise<Uint8Array> {
  if (typeof Worker === "undefined") {
    const { exportSkin, parseSkin } = await import("./package");
    const bytes = exportSkin(skin); parseSkin(bytes); return bytes;
  }
  return run<Uint8Array>({ type: "export", skin });
}
