import { readFileSync } from "node:fs";
import { parseSkin, exportSkin } from "./package";

// Reuse the existing tracked PNG fixture; do not require a local skin package.
export function layeredFixture() {
  const base = parseSkin(new Uint8Array(readFileSync("public/skins/mux-starter.muxskin")));
  const png = base.files[base.manifest.states.idle.src];
  return parseSkin(exportSkin({
    manifest: { ...base.manifest, schemaVersion: 3, preview: "body.png", states: { idle: { type: "image", src: "body.png" } }, layered: { layers: [
      { id: "body", src: "body.png", role: "body", x: 0, y: 0, width: 1, height: 1, pivot: { x: .5, y: .5 } },
      { id: "eyes", src: "eyes.png", role: "eyes", x: 0, y: 0, width: 1, height: 1, pivot: { x: .5, y: .5 } },
      { id: "mouth", src: "mouth.png", role: "mouth", x: 0, y: 0, width: 1, height: 1, pivot: { x: .5, y: .5 } },
      { id: "node", src: "node.png", role: "decoration", x: 0, y: 0, width: 1, height: 1, pivot: { x: .5, y: .5 }, motions: { thinking: { duration: 1200, loop: true, frames: [{ opacity: 1 }, { opacity: .3 }, { opacity: 1 }] } } },
    ] } }, files: { "body.png": png, "eyes.png": png, "mouth.png": png, "node.png": png },
  }));
}
