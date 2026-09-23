import { exportSkin, parseSkin } from "./package";
import type { Skin } from "./package";
self.onmessage = (event: MessageEvent<{ type: "parse"; bytes: Uint8Array } | { type: "export"; skin: Skin }>) => {
  try {
    if (event.data.type === "parse") {
      const skin = parseSkin(event.data.bytes);
      self.postMessage({ result: skin }, { transfer: Object.values(skin.files).map(bytes => bytes.buffer as ArrayBuffer) });
    } else {
      const bytes = exportSkin(event.data.skin);
      parseSkin(bytes);
      self.postMessage({ result: bytes }, { transfer: [bytes.buffer] });
    }
  } catch (error) { self.postMessage({ error: error instanceof Error ? error.message : String(error) }); }
};
