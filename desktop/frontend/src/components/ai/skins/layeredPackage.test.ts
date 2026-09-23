import { layeredFixture } from "./layeredFixture";
import { describe, expect, it } from "vitest";
import { zipSync, strToU8 } from "fflate";
import { parseSkin, exportSkin, validateManifest } from "./package";
const sample = layeredFixture;
describe("layered skin packages", () => {
  it("imports a layered fixture and round-trips geometry and images", () => {
    const skin = sample();
    expect(skin.manifest.layered!.layers).toHaveLength(4);
    expect(skin.manifest.layered!.layers[0].width).toBe(1);
    const restored = parseSkin(exportSkin(skin));
    expect(restored.manifest).toEqual(skin.manifest);
    for (const layer of skin.manifest.layered!.layers) expect(restored.files[layer.src]).toEqual(skin.files[layer.src]);
  });
  it("rejects missing layer images", () => {
    const skin = sample(); delete skin.files["eyes.png"];
    expect(() => parseSkin(exportSkin(skin))).toThrow(/缺少部件/);
  });
  it.each(["duplicate", "no-body", "remote", "infinity", "frames", "script", "version", "mixed"])("rejects invalid definition: %s", kind => {
    const m: any = structuredClone(sample().manifest), layers = m.layered.layers;
    if (kind === "duplicate") layers[1].id = layers[0].id;
    if (kind === "no-body") layers[0].role = "eyes";
    if (kind === "remote") layers[0].src = "https://example.com/a.png";
    if (kind === "infinity") layers[0].width = Infinity;
    if (kind === "frames") layers[3].motions.thinking.frames = Array(17).fill({});
    if (kind === "script") layers[3].motions.thinking.frames[0].transform = "url(x)";
    if (kind === "version") m.schemaVersion = 1;
    if (kind === "mixed") m.live2d = { model: "model.model3.json" };
    expect(() => validateManifest(m)).toThrow();
  });
  it("rejects malformed PNG layer data even when ZIP CRC is valid", () => {
    const skin = sample(); skin.files["eyes.png"] = strToU8("not a PNG");
    expect(() => parseSkin(zipSync({ ...skin.files, "manifest.json": strToU8(JSON.stringify(skin.manifest)) }))).toThrow(/PNG/);
  });
});
