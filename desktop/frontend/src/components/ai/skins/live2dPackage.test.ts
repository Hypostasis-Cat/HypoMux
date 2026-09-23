import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import { unzipSync, zipSync, strToU8 } from "fflate";
import { parseSkin } from "./package";
import { readLive2DModel } from "./live2dPackage";

function fixture() {
  const files = unzipSync(new Uint8Array(readFileSync(new URL("../../../../public/skins/mux-starter.muxskin", import.meta.url))));
  const manifest = { ...JSON.parse(new TextDecoder().decode(files["manifest.json"])), schemaVersion: 2, live2d: { model: "model.model3.json", motions: { hover: { group: "Hello", index: 0 } } } };
  const model = { Version: 3, FileReferences: { Moc: "model.moc3", Textures: ["assets/idle.png"], Motions: { Hello: [{ File: "hello.motion3.json", Sound: "https://untrusted/audio.mp3", Command: "open_url https://untrusted" }] } } };
  files["manifest.json"] = strToU8(JSON.stringify(manifest));
  files["model.moc3"] = strToU8("MOC3test");
  files["hello.motion3.json"] = strToU8('{"Version":3}');
  return { files, model, pack: () => zipSync({ ...files, "model.model3.json": strToU8(JSON.stringify(model)) }) };
}
it("imports Live2D alongside fallback images and strips viewer commands and audio", () => {
  const f = fixture(), skin = parseSkin(f.pack());
  expect(skin.manifest.schemaVersion).toBe(2);
  const model = readLive2DModel(skin.manifest.live2d!, skin.files);
  expect(model.FileReferences.Motions.Hello).toEqual([{ File: "hello.motion3.json" }]);
});
it.each(["https://host/model.moc3", "../model.moc3", "file:///model.moc3", "missing.moc3"])("rejects nonlocal or missing model reference %s", path => {
  const f = fixture(); f.model.FileReferences.Moc = path;
  expect(() => parseSkin(f.pack())).toThrow(/Live2D/);
});
it("rejects missing motions and malformed JSON before model loading", () => {
  const f = fixture(); delete f.files["hello.motion3.json"];
  expect(() => parseSkin(f.pack())).toThrow(/Live2D/);
  f.files["hello.motion3.json"] = strToU8("invalid");
  expect(() => parseSkin(f.pack())).toThrow(/Live2D/);
});
it("rejects invalid motion mappings", () => {
  const f = fixture(); f.model.FileReferences.Motions.Hello = [];
  expect(() => parseSkin(f.pack())).toThrow(/Live2D/);
});
