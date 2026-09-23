import { Unzip, UnzipInflate, zipSync, strFromU8, strToU8 } from "fflate";

export const skinStates = ["idle", "thinking", "waiting", "replying", "hover", "dragging"] as const;
export type SkinState = typeof skinStates[number];
export type SkinAnimation = { type: "image"; src: string } | { type: "spritesheet"; src: string; columns: number; frames: number; fps: number; loop: boolean };
export interface SkinManifest {
  schemaVersion: 1; id: string; name: string; author: string; version: string;
  preview?: string; canvas: { width: number; height: number }; anchor: { x: number; y: number };
  states: Partial<Record<SkinState, SkinAnimation>> & { idle: SkinAnimation };
}
export interface Skin { manifest: SkinManifest; files: Record<string, Uint8Array> }
export const limits = { package: 20 * 1024 * 1024, expanded: 32 * 1024 * 1024, files: 32, pixels: 16 * 1024 * 1024 };
const fail = (message: string): never => { throw new Error(message); };
const integer = (n: unknown, min: number, max: number): n is number => Number.isInteger(n) && Number(n) >= min && Number(n) <= max;
const record = (v: unknown): v is Record<string, any> => !!v && typeof v === "object" && !Array.isArray(v);
const shortText = (v: unknown, max = 80): v is string => typeof v === "string" && v.trim().length > 0 && v.length <= max && !/[\x00-\x1f]/.test(v);
export function safePath(path: string): boolean {
  return path.length <= 160 && /^[A-Za-z0-9_./-]+$/.test(path) && !path.startsWith("/") && path.split("/").every(part => part !== "" && part !== "." && part !== "..");
}
export function validateManifest(value: unknown): SkinManifest {
  if (!record(value) || value.schemaVersion !== 1) fail("Unsupported skin format / 不支持的皮肤格式版本");
  const v = value as Record<string, any>;
  if (!shortText(v.id) || !/^[a-z0-9][a-z0-9.-]{2,79}$/.test(v.id) || ["builtin.default", "preferences"].includes(v.id)) fail("Invalid skin ID / 皮肤 ID 无效");
  if (!shortText(v.name) || !shortText(v.author) || !shortText(v.version, 32)) fail("Invalid name, author or version / 名称、作者或版本无效");
  if (!record(v.canvas) || !integer(v.canvas.width, 32, 2048) || !integer(v.canvas.height, 32, 2048) || v.canvas.width / v.canvas.height < 0.25 || v.canvas.width / v.canvas.height > 4) fail("Canvas must be 32–2048 px, aspect ratio 1:4–4:1 / 画布尺寸或比例超出范围");
  if (!record(v.anchor) || ![v.anchor.x, v.anchor.y].every(n => typeof n === "number" && Number.isFinite(n) && n >= 0 && n <= 1)) fail("Anchor must be between 0 and 1 / 锚点必须在 0–1 之间");
  const pngPath = (p: unknown) => typeof p === "string" && safePath(p) && p.endsWith(".png");
  if (v.preview !== undefined && !pngPath(v.preview)) fail("Invalid preview path / 预览图路径无效");
  if (!record(v.states) || !v.states.idle || Object.keys(v.states).some(s => !skinStates.includes(s as SkinState))) fail("Missing idle or unknown state / 缺少待机形象或状态名无效");
  const states: Partial<Record<SkinState, SkinAnimation>> = {};
  for (const key of skinStates) {
    const a = v.states[key];
    if (a === undefined) continue;
    if (!record(a) || !pngPath(a.src)) fail(`Invalid state / 状态无效: ${key}`);
    if (a.type === "image") states[key] = { type: "image", src: a.src };
    else if (a.type === "spritesheet" && integer(a.columns, 1, 64) && integer(a.frames, 1, 120) && a.columns <= a.frames && integer(a.fps, 1, 30) && typeof a.loop === "boolean") states[key] = { type: "spritesheet", src: a.src, columns: a.columns, frames: a.frames, fps: a.fps, loop: a.loop };
    else fail(`Invalid animation / 动画参数无效: ${key}`);
  }
  return { schemaVersion: 1, id: v.id, name: v.name, author: v.author, version: v.version, ...(v.preview ? { preview: v.preview } : {}), canvas: { width: v.canvas.width, height: v.canvas.height }, anchor: { x: v.anchor.x, y: v.anchor.y }, states: states as SkinManifest["states"] };
}

export function pngSize(bytes: Uint8Array): { width: number; height: number } {
  if (bytes.length < 45 || ![137,80,78,71,13,10,26,10].every((n, i) => bytes[i] === n)) fail("Only static PNG images are supported / 仅支持静态 PNG 图片");
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  if (view.getUint32(8) !== 13 || strFromU8(bytes.subarray(12, 16)) !== "IHDR") fail("Invalid PNG header / PNG 文件头无效");
  const width = view.getUint32(16), height = view.getUint32(20);
  if (!width || !height || width > 8192 || height > 8192 || width * height > limits.pixels) fail("PNG dimensions too large / PNG 尺寸过大");
  let offset = 8, ended = false;
  while (offset + 12 <= bytes.length) {
    const length = view.getUint32(offset), type = strFromU8(bytes.subarray(offset + 4, offset + 8));
    if (offset + length + 12 > bytes.length) fail("Truncated PNG / PNG 文件不完整");
    if (type === "acTL") fail("Use spritesheets instead of APNG / 动画请使用精灵图，暂不支持 APNG");
    if (crc32(bytes.subarray(offset + 4, offset + 8 + length)) !== view.getUint32(offset + 8 + length)) fail("PNG checksum mismatch / PNG 校验失败");
    offset += length + 12;
    if (type === "IEND") { ended = true; break; }
  }
  if (!ended || offset !== bytes.length) fail("Invalid PNG ending / PNG 结尾无效");
  return { width, height };
}
const crcTable = Uint32Array.from({ length: 256 }, (_, n) => { for (let i = 0; i < 8; i++) n = (n & 1) ? 0xedb88320 ^ (n >>> 1) : n >>> 1; return n >>> 0; });
function crc32(bytes: Uint8Array): number { let crc = 0xffffffff; for (const b of bytes) crc = crcTable[(crc ^ b) & 255] ^ (crc >>> 8); return (crc ^ 0xffffffff) >>> 0; }

// Inspect central and local headers before allocating decompression buffers.
function inspectZip(bytes: Uint8Array): Map<string, { size: number; crc: number }> {
  if (bytes.length < 22 || bytes.length > limits.package) fail("Skin package must be under 20 MiB / 皮肤包须小于 20 MiB");
  const v = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let end = bytes.length - 22;
  while (end >= Math.max(0, bytes.length - 65557) && v.getUint32(end, true) !== 0x06054b50) end--;
  if (end < 0 || v.getUint32(end, true) !== 0x06054b50 || end + 22 + v.getUint16(end + 20, true) !== bytes.length) fail("Invalid ZIP ending / ZIP 文件不完整");
  const count = v.getUint16(end + 10, true), start = v.getUint32(end + 16, true);
  if (v.getUint16(end + 4, true) || v.getUint16(end + 6, true) || v.getUint16(end + 8, true) !== count || count > limits.files || start + v.getUint32(end + 12, true) !== end) fail("Unsupported ZIP structure / 不支持的 ZIP 结构");
  const entries = new Map<string, { size: number; crc: number }>();
  let p = start, total = 0;
  for (let i = 0; i < count; i++) {
    if (p + 46 > end || v.getUint32(p, true) !== 0x02014b50) fail("Invalid ZIP entry / ZIP 条目无效");
    const flags = v.getUint16(p + 8, true), method = v.getUint16(p + 10, true), compressed = v.getUint32(p + 20, true), size = v.getUint32(p + 24, true);
    const nameLength = v.getUint16(p + 28, true), extra = v.getUint16(p + 30, true), comment = v.getUint16(p + 32, true), local = v.getUint32(p + 42, true);
    if (p + 46 + nameLength + extra + comment > end) fail("Truncated ZIP entry / ZIP 条目不完整");
    const name = strFromU8(bytes.subarray(p + 46, p + 46 + nameLength));
    const directory = name.endsWith("/");
    if (!safePath(directory ? name.slice(0, -1) : name) || entries.has(name) || (!directory && name !== "manifest.json" && !name.endsWith(".png"))) fail(`Unsupported or duplicate path / 不支持或重复的路径: ${name.slice(0, 160)}`);
    if ((flags & ~0x080e) || ![0, 8].includes(method) || v.getUint16(p + 34, true) || ((v.getUint32(p + 38, true) >>> 16) & 0xf000) === 0xa000) fail("Encrypted files and links are not supported / 不支持加密文件或链接");
    total += size;
    if (total > limits.expanded || (name === "manifest.json" && size > 16384) || (directory && size !== 0)) fail("Expanded package too large / 解压后文件过大");
    if (local + 30 > start || v.getUint32(local, true) !== 0x04034b50 || v.getUint16(local + 6, true) !== flags || v.getUint16(local + 8, true) !== method) fail("Invalid local ZIP header / ZIP 本地文件头无效");
    const ln = v.getUint16(local + 26, true), le = v.getUint16(local + 28, true);
    if (local + 30 + ln + le + compressed > start || strFromU8(bytes.subarray(local + 30, local + 30 + ln)) !== name) fail("Invalid ZIP file bounds / ZIP 文件边界无效");
    entries.set(name, { size, crc: v.getUint32(p + 16, true) });
    p += 46 + nameLength + extra + comment;
  }
  if (p !== end || !entries.has("manifest.json")) fail("manifest.json must be at the root / 根目录必须包含 manifest.json");
  return entries;
}
export function parseSkin(bytes: Uint8Array): Skin {
  const entries = inspectZip(bytes);
  const unpacked: Record<string, Uint8Array> = Object.create(null);
  const seen = new Set<string>();
  const unzip = new Unzip(file => {
    const expected = entries.get(file.name);
    if (!expected || seen.has(file.name)) fail("Unexpected ZIP entry / ZIP 条目与目录不一致");
    const expectedSize = expected!.size;
    seen.add(file.name);
    const chunks: Uint8Array[] = []; let size = 0;
    file.ondata = (error, data, final) => {
      if (error) throw error;
      size += data.length;
      if (size > expectedSize) { file.terminate(); fail("Expanded size mismatch / 实际解压大小超出声明"); }
      chunks.push(data);
      if (final) {
        if (size !== expectedSize) fail("Truncated ZIP data / ZIP 数据不完整");
        const result = new Uint8Array(size); let offset = 0;
        for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.length; }
        unpacked[file.name] = result;
      }
    };
    file.start();
  });
  unzip.register(UnzipInflate);
  // Small input chunks bound inflater allocation even when ZIP sizes are forged.
  for (let i = 0; i < bytes.length; i += 1024) unzip.push(bytes.subarray(i, i + 1024), i + 1024 >= bytes.length);
  const files: Record<string, Uint8Array> = Object.create(null);
  for (const [name, info] of entries) {
    const data = unpacked[name];
    if (!data || data.length !== info.size || crc32(data) !== info.crc) fail(`ZIP checksum mismatch / ZIP 校验失败: ${name}`);
    if (!name.endsWith("/")) files[name] = data;
  }
  let value: unknown;
  try { value = JSON.parse(strFromU8(files["manifest.json"])); } catch { fail("Invalid manifest JSON / 描述文件不是有效 JSON"); }
  const manifest = validateManifest(value);
  let pixels = 0;
  const sizes = new Map<string, { width: number; height: number }>();
  for (const [path, data] of Object.entries(files)) if (path.endsWith(".png")) {
    const size = pngSize(data); pixels += size.width * size.height; sizes.set(path, size);
  }
  if (pixels > limits.pixels) fail("Total decoded images exceed 16 megapixels / 图片总像素超过 1600 万");
  if (manifest.preview && !sizes.has(manifest.preview)) fail("Missing preview / 缺少预览图");
  for (const animation of Object.values(manifest.states)) {
    const size = sizes.get(animation.src), columns = animation.type === "image" ? 1 : animation.columns, rows = animation.type === "image" ? 1 : Math.ceil(animation.frames / columns);
    if (!size || size.width !== manifest.canvas.width * columns || size.height !== manifest.canvas.height * rows) fail(`Image dimensions must match canvas and frame grid / 图片尺寸须匹配画布与帧网格: ${animation.src}`);
  }
  return { manifest, files };
}
export function exportSkin(skin: Skin): Uint8Array {
  return zipSync({ ...skin.files, "manifest.json": new Uint8Array(strToU8(JSON.stringify(skin.manifest, null, 2))) }, { level: 6 });
}
export async function verifyImages(skin: Skin): Promise<void> {
  for (const [path, bytes] of Object.entries(skin.files)) if (path.endsWith(".png")) {
    const url = URL.createObjectURL(new Blob([new Uint8Array(bytes)], { type: "image/png" }));
    try { const img = new Image(); img.src = url; await img.decode(); } catch { fail(`Cannot decode image / 无法解码图片: ${path}`); } finally { URL.revokeObjectURL(url); }
  }
}
