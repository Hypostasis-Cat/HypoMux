import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { unzipSync, zipSync, strToU8 } from "fflate";
import { exportSkin, parseSkin, safePath, validateManifest } from "./package";

const starter = new Uint8Array(readFileSync(new URL("../../../../public/skins/mux-starter.muxskin", import.meta.url)));
const files = () => unzipSync(starter);
const manifest = () => parseSkin(starter).manifest;
describe("skin packages", () => {
  it("imports a compressed creator pack and round-trips export", () => {
    const skin = parseSkin(starter);
    expect(skin.manifest.states.thinking?.type).toBe("spritesheet");
    expect(parseSkin(exportSkin(skin))).toEqual(skin);
  });
  it("accepts explicit directory entries", () => {
    expect(parseSkin(zipSync({ ...files(), "assets/": new Uint8Array() })).manifest.id).toBe("org.hypomux.starter");
  });
  it.each(["../evil.png", "/evil.png", "C:/evil.png", "assets\\evil.png", "assets//evil.png", "https://x/evil.png", "assets/./evil.png", "assets/%2e%2e/evil.png"])("rejects unsafe path %s", path => {
    expect(safePath(path)).toBe(false);
    expect(() => parseSkin(zipSync({ ...files(), [path]: new Uint8Array() }))).toThrow();
  });
  it("rejects executable and SVG resources", () => {
    for (const path of ["run.js", "assets/character.svg", "index.html"]) expect(() => parseSkin(zipSync({ ...files(), [path]: strToU8("test") }))).toThrow();
  });
  it("rejects missing images and mismatched frame grids", () => {
    const f = files(); delete f["assets/thinking.png"];
    expect(() => parseSkin(zipSync(f))).toThrow(/尺寸/);
    const m = manifest(); m.canvas.width = 64;
    expect(() => parseSkin(zipSync({ ...files(), "manifest.json": strToU8(JSON.stringify(m)) }))).toThrow(/尺寸/);
  });
  it("rejects invalid PNG data even with a valid ZIP checksum", () => {
    const f = files(); f["assets/idle.png"][60] ^= 1;
    expect(() => parseSkin(zipSync(f))).toThrow(/PNG/);
  });
  it("rejects truncation and ZIP checksum corruption", () => {
    expect(() => parseSkin(starter.subarray(0, starter.length - 5))).toThrow();
    const bytes = exportSkin(parseSkin(starter));
    bytes[60] ^= 1;
    expect(() => parseSkin(bytes)).toThrow();
  });
  it("rejects oversized expanded data before inflating", () => {
    const bytes = starter.slice(), v = new DataView(bytes.buffer);
    for (let i = 0; i < bytes.length - 46; i++) if (v.getUint32(i, true) === 0x02014b50) { v.setUint32(i + 24, 40 * 1024 * 1024, true); break; }
    expect(() => parseSkin(bytes)).toThrow(/过大/);
  });
  it("rejects forged smaller sizes while streaming", () => {
    const bytes = starter.slice(), v = new DataView(bytes.buffer);
    for (let i = 0; i < bytes.length - 46; i++) if (v.getUint32(i, true) === 0x02014b50) { v.setUint32(i + 24, 1, true); break; }
    expect(() => parseSkin(bytes)).toThrow(/解压大小/);
  });
  it("rejects too many entries", () => {
    const f = files(); for (let i = 0; i < 33; i++) f[`assets/${i}.png`] = new Uint8Array();
    expect(() => parseSkin(zipSync(f))).toThrow();
  });
  it.each([
    { schemaVersion: 2 }, { id: "builtin.default" }, { id: "preferences" }, { name: "" }, { author: "" },
    { canvas: { width: 0, height: 128 } }, { canvas: { width: 2048, height: 32 } },
    { anchor: { x: NaN, y: 1 } }, { anchor: { x: 0.5, y: 2 } }, { states: {} },
    { states: { idle: { type: "image", src: "https://host/a.png" } } },
    { states: { idle: { type: "spritesheet", src: "idle.png", columns: 1, frames: 121, fps: 30, loop: true } } },
  ])("rejects invalid metadata %j", patch => expect(() => validateManifest({ ...manifest(), ...patch })).toThrow());
});
