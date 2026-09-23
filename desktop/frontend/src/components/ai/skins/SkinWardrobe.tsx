import { useEffect, useRef, useState } from "react";
import { Button, Checkbox, Field, Input } from "@fluentui/react-components";
import { Add20Regular, ArrowDownload20Regular, ArrowUpload20Regular, Checkmark16Regular, Delete20Regular, Image20Regular } from "@fluentui/react-icons";
import { saveCompanionFile } from "../../../platform/companionExport";
import { useI18n } from "../../../i18n/i18n";
import { DefaultCharacter } from "./DefaultCharacter";
import { SkinCharacter } from "./SkinCharacter";
import { exportSkin, limits, parseSkin, pngSize, skinStates, validateManifest, verifyImages, type Skin, type SkinState } from "./package";
import { getSkinSize, installSkin, loadSkins, removeSkin, savePreferences, saveSkinSize, useSkins } from "./store";
import "./skins.css";

export function SkinWardrobe() {
  const { locale } = useI18n();
  const { skins, preferences, loaded, error: storageError } = useSkins();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [candidate, setCandidate] = useState<Skin>();
  const [inspectedId, setInspectedId] = useState<string>();
  const [creating, setCreating] = useState(false);
  const [state, setState] = useState<SkinState>("idle");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const previewId = candidate?.manifest.id ?? inspectedId ?? preferences.selected;
  const previewSize = getSkinSize(preferences, previewId);
  const [size, setSize] = useState(previewSize);
  const fileInput = useRef<HTMLInputElement>(null);
  const imageInput = useRef<HTMLInputElement>(null);
  const operation = useRef(false);
  const pendingSize = useRef<number>();
  const draftSize = useRef<number>();
  const sizeSaveId = useRef(0);
  const savedSize = useRef(previewSize);
  savedSize.current = previewSize;
  const sizeOwner = useRef(previewId);
  useEffect(() => {
    if (sizeOwner.current !== previewId) {
      sizeOwner.current = previewId;
      sizeSaveId.current++;
      draftSize.current = undefined;
      pendingSize.current = undefined;
    }
    if (draftSize.current === undefined) setSize(previewSize);
  }, [previewId, previewSize]);
  const preview = candidate ?? skins.find(skin => skin.manifest.id === previewId);
  const active = !candidate && previewId === preferences.selected;
  const ratio = preview ? preview.manifest.canvas.width / preview.manifest.canvas.height : 1;
  const labels = [text("待机", "Idle"), text("思考", "Thinking"), text("待确认", "Approval"), text("回复", "Reply"), text("悬停", "Hover"), text("拖动", "Drag")];
  const captions: Record<SkinState, string> = {
    idle: text("今天也一起出发吧。", "Ready for another adventure."),
    thinking: text("让我想一想…", "Let me think…"),
    waiting: text("准备好了，等你确认。", "Ready when you are."),
    replying: text("有结果啦，来看看！", "Here is what I found!"),
    hover: text("我在这里，随时叫我。", "Right here if you need me."),
    dragging: text("好，我们换个位置。", "A little change of scenery."),
  };
  const action = async (work: () => Promise<void>) => {
    if (operation.current) return;
    operation.current = true; setBusy(true); setError(""); setNotice("");
    try { await work(); } catch (error) { setError(error instanceof Error ? error.message : String(error)); }
    finally { operation.current = false; setBusy(false); }
  };
  const saveFile = async (name: string, bytes: Uint8Array) => {
    const result = await saveCompanionFile(name, bytes);
    setNotice(result === "saved" ? text("文件已保存。", "File saved.") : result === "cancelled" ? text("已取消导出。", "Export cancelled.") : text("已请求下载，请检查浏览器下载列表。", "Download requested. Check your browser downloads."));
  };
  const download = (skin: Skin) => saveFile(`${skin.manifest.id}.muxskin`, exportSkin(skin));
  const resource = async (name: string) => {
    const response = await fetch(`/skins/${name}`);
    if (!response.ok) throw new Error(text("无法读取示例资源", "Cannot read starter resource"));
    await saveFile(name, new Uint8Array(await response.arrayBuffer()));
  };
  const importFile = (file: File, image: boolean) => action(async () => {
    if (file.size > limits.package) throw new Error(text("文件不能超过 20 MiB", "File must not exceed 20 MiB"));
    const bytes = new Uint8Array(await file.arrayBuffer());
    let skin: Skin;
    if (image) {
      const canvas = pngSize(bytes);
      skin = { manifest: validateManifest({ schemaVersion: 1, id: `local.${crypto.randomUUID()}`, name: file.name.replace(/\.png$/i, "").slice(0, 80) || "My Mux", author: text("本机创作者", "Local creator"), version: "1.0.0", preview: "assets/idle.png", canvas, anchor: { x: 0.5, y: 1 }, states: { idle: { type: "image", src: "assets/idle.png" } } }), files: { "assets/idle.png": bytes } };
    } else skin = parseSkin(bytes);
    await verifyImages(skin);
    setCandidate(skin); setCreating(image); setState("idle"); setDeleting(false);
  });
  const inspect = (id: string) => { setInspectedId(id); setCandidate(undefined); setCreating(false); setState("idle"); setDeleting(false); setNotice(""); };
  const updateCreator = (patch: Partial<Skin["manifest"]>) => { if (candidate) setCandidate({ ...candidate, manifest: { ...candidate.manifest, ...patch } }); };
  const apply = () => action(async () => {
    if (candidate) await installSkin(candidate);
    else await savePreferences({ selected: previewId });
    if (size !== previewSize) await saveSkinSize(previewId, size);
    setCandidate(undefined); setCreating(false); setInspectedId(previewId);
    setNotice(text("新形象已应用，小 Mux 准备好啦。", "Your new companion is ready."));
  });
  const commitSize = () => {
    const next = draftSize.current ?? size;
    if (next === pendingSize.current) return;
    if (next === previewSize && pendingSize.current === undefined) { draftSize.current = undefined; return; }
    const request = ++sizeSaveId.current;
    pendingSize.current = next;
    void saveSkinSize(previewId, next).then(() => {
      if (sizeSaveId.current !== request) return;
      pendingSize.current = undefined;
      if (draftSize.current === next) draftSize.current = undefined;
    }).catch(error => {
      if (sizeSaveId.current !== request) return;
      pendingSize.current = undefined;
      if (draftSize.current === next) { draftSize.current = undefined; setSize(savedSize.current); }
      setError(error instanceof Error ? error.message : String(error));
    });
  };
  const renderCharacter = (skin: Skin | undefined, animated = false) => skin
    ? <SkinCharacter skin={skin} state={animated ? state : "idle"} animate={animated && preferences.animate} posterOnly={!animated} fallback={<DefaultCharacter />} />
    : <DefaultCharacter />;

  return <div className="ai-settings mux-wardrobe">
    <input hidden ref={fileInput} type="file" accept=".muxskin,.zip" onChange={event => { const file = event.target.files?.[0]; event.target.value = ""; if (file) void importFile(file, false); }} />
    <input hidden ref={imageInput} type="file" accept=".png,image/png" onChange={event => { const file = event.target.files?.[0]; event.target.value = ""; if (file) void importFile(file, true); }} />
    <header className="mux-wardrobe-heading">
      <div><span className="mux-eyebrow">COMPANION / WARDROBE</span><h2>{text("小 Mux 衣柜", "Mux wardrobe")}</h2><p>{text("换个喜欢的形象，让陪伴更有趣。", "A familiar companion. A little more you.")}</p></div>
      <div className="mux-header-actions"><Button icon={<Image20Regular />} disabled={busy || !loaded} onClick={() => imageInput.current?.click()}>{text("从图片创建", "Create from image")}</Button><Button appearance="primary" icon={<ArrowUpload20Regular />} disabled={busy || !loaded} onClick={() => fileInput.current?.click()}>{text("导入皮肤包", "Import skin pack")}</Button></div>
    </header>
    {(error || storageError) && <div role="alert" className="mux-notice mux-notice-error">{error || storageError}{storageError && <Button size="small" onClick={() => void loadSkins()}>{text("重新读取", "Retry")}</Button>}</div>}
    {notice && <div className="mux-notice" role="status"><Checkmark16Regular />{notice}</div>}
    <div className="mux-wardrobe-layout">
      <section className="mux-preview-panel" aria-label={text("角色预览", "Character preview")}>
        <div className="mux-preview-stage" data-live2d={!!preview?.manifest.live2d} data-state={state} data-skin-animate={preferences.animate}>
          <div className="mux-stage-top"><span className="mux-preview-caption">{candidate ? text("安装前预览", "Import preview") : text("形象预览", "Character preview")}</span><span className="mux-stage-state"><i />{labels[skinStates.indexOf(state)]}</span></div>
          <span className="mux-preview-dialogue">{captions[state]}</span>
          <div className="mux-preview-character" style={{ width: size * Math.min(1, ratio), height: size * Math.min(1, 1 / ratio) }}>
            {renderCharacter(preview, true)}
            {creating && candidate && <span className="mux-anchor-marker" style={{ left: `${candidate.manifest.anchor.x * 100}%`, top: `${candidate.manifest.anchor.y * 100}%` }} aria-hidden="true" />}
          </div>
          <span className="mux-stage-ground" aria-hidden="true" />
          <span className="mux-stage-footnote">{text("实际显示大小", "Actual display size")} · {size}px</span>
        </div>
        <div className="mux-preview-body">
          <div className="mux-preview-identity"><div><h3>{preview?.manifest.name ?? text("小 Mux", "Mux")}</h3><p>{preview ? `${preview.manifest.author} · v${preview.manifest.version}` : text("HypoMux 原生形象", "The original HypoMux companion")}</p></div>{active && <span className="mux-active-label"><Checkmark16Regular />{text("使用中", "Active")}</span>}{candidate && <span className="mux-draft-label">{text("未安装", "Not installed")}</span>}</div>
          <div className="mux-state-buttons" role="group" aria-label={text("预览动作", "Preview action")}>{skinStates.map((name, index) => <button key={name} type="button" aria-pressed={state === name} onClick={() => setState(name)}><span className={`mux-state-dot mux-state-dot-${name}`} />{labels[index]}</button>)}</div>
          <p className="mux-state-note">{preview?.manifest.layered ? text("分层动态 · 自动眨眼、呼吸，回复时嘴部会动；可切换状态预览。", "Layered · Automatic blinking, breathing and speaking. Try each state.") : preview?.manifest.live2d ? text("Live2D · 移动鼠标与它对视，切换状态体验模型动作。", "Live2D · Move your pointer for eye contact; select a state to try its motion.") : preview && !preview.manifest.states[state] ? text("此动作使用待机形象。", "This action uses the idle image.") : text("点击上方动作，看看它的不同表情。", "Try an action to see its expression.")}</p>
          <div className="mux-preferences"><div className="mux-size-field"><div className="mux-size-label"><span>{text("角色大小", "Character size")}</span><output>{size}px</output></div><input aria-label={text("角色大小", "Character size")} type="range" min="72" max="200" step="8" value={size} disabled={!loaded} onChange={event => { const next = Number(event.target.value); draftSize.current = next; setSize(next); }} onPointerUp={commitSize} onKeyUp={commitSize} onBlur={commitSize} /></div><Checkbox label={text("播放角色动画", "Animate character")} checked={preferences.animate} disabled={busy || !loaded} onChange={(_, data) => void action(() => savePreferences({ animate: data.checked === true }))} /></div>
          <div className="mux-preview-actions"><Button appearance="primary" disabled={busy || !loaded || active} onClick={() => void apply()}>{candidate ? text("安装并应用", "Install and apply") : active ? text("正在使用此形象", "Currently active") : text("应用此形象", "Apply character")}</Button>{preview && <Button appearance="subtle" icon={<ArrowDownload20Regular />} title={text("导出皮肤包", "Export skin pack")} aria-label={text("导出皮肤包", "Export skin pack")} disabled={busy} onClick={() => void action(async () => { validateManifest(preview.manifest); await download(preview); })} />}{preview && !candidate && <Button appearance="subtle" icon={<Delete20Regular />} title={text("删除皮肤", "Delete skin")} aria-label={text("删除皮肤", "Delete skin")} disabled={busy} onClick={() => setDeleting(true)} />}{candidate && <Button disabled={busy} onClick={() => { setCandidate(undefined); setCreating(false); }}>{text("取消", "Cancel")}</Button>}</div>
          {deleting && preview && <div className="mux-delete-confirm"><p>{text("删除这个本机皮肤？使用中的形象会恢复默认。", "Delete this local skin? An active skin will revert to default.")}</p><Button size="small" disabled={busy} onClick={() => void action(async () => { await removeSkin(preview.manifest.id); setDeleting(false); setInspectedId(undefined); })}>{text("确认删除", "Delete skin")}</Button><Button size="small" disabled={busy} onClick={() => setDeleting(false)}>{text("取消", "Cancel")}</Button></div>}
        </div>
      </section>
      <div className="mux-wardrobe-content">
        {candidate && <section className="mux-candidate"><div className="mux-section-heading"><h3>{creating ? text("制作你的皮肤", "Make it yours") : text("皮肤包已就绪", "Your skin is ready")}</h3><span className="mux-count">{(candidate.manifest.layered ? 6 : Object.keys({ ...candidate.manifest.states, ...candidate.manifest.live2d?.motions }).length)} {text("种状态", "states")}</span></div><p>{text("在左侧预览效果，满意后点击「安装并应用」。", "Preview on the left, then install when you are happy with it.")}</p>
          {creating && <><div className="mux-creator-fields"><Field label={text("皮肤名称", "Skin name")}><Input value={candidate.manifest.name} maxLength={80} onChange={(_, data) => updateCreator({ name: data.value })} /></Field><Field label={text("作者", "Author")}><Input value={candidate.manifest.author} maxLength={80} onChange={(_, data) => updateCreator({ author: data.value })} /></Field></div><details className="mux-anchor-options"><summary>{text("调整气泡位置", "Adjust the speech bubble")}</summary><p>{text("预览中的圆点是气泡参考位置。", "The preview dot marks the bubble anchor.")}</p><Field label={text("水平位置", "Horizontal position")}><input aria-label={text("水平锚点", "Horizontal anchor")} type="range" min="0" max="1" step="0.05" value={candidate.manifest.anchor.x} onChange={event => updateCreator({ anchor: { ...candidate.manifest.anchor, x: Number(event.target.value) } })} /></Field><Field label={text("垂直位置", "Vertical position")}><input aria-label={text("垂直锚点", "Vertical anchor")} type="range" min="0" max="1" step="0.05" value={candidate.manifest.anchor.y} onChange={event => updateCreator({ anchor: { ...candidate.manifest.anchor, y: Number(event.target.value) } })} /></Field></details></>}
          {skins.some(skin => skin.manifest.id === candidate.manifest.id) && <p className="mux-replacement-note">{text("已安装同 ID 皮肤，应用后将替换原版本。", "This will replace the installed skin with the same ID.")}</p>}
        </section>}
        <section className="mux-library" data-drag-over={dragOver} onDragOver={event => { if (event.dataTransfer.types.includes("Files")) { event.preventDefault(); setDragOver(true); } }} onDragLeave={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setDragOver(false); }} onDrop={event => { event.preventDefault(); setDragOver(false); if (busy) return; const files = event.dataTransfer.files; if (files.length !== 1) { setError(text("请每次导入一个文件。", "Import one file at a time.")); return; } void importFile(files[0], /\.png$/i.test(files[0].name)); }}>
          <div className="mux-section-heading"><h3>{text("我的皮肤", "Your collection")} <span className="mux-count">{skins.length + 1}</span></h3><span className="mux-local-label"><i />{text("本机收藏", "On this device")}</span></div>
          <p className="mux-library-hint">{text("选择一款皮肤预览，也可以把皮肤包拖到这里。", "Select a character to preview, or drop a skin pack here.")}</p>
          {!loaded && <p role="status">{text("正在读取皮肤…", "Loading skins…")}</p>}
          <div className="mux-skin-grid">
            {[undefined, ...skins].map(skin => {
              const id = skin?.manifest.id ?? "builtin.default";
              const name = skin?.manifest.name ?? text("小 Mux", "Mux");
              const isActive = preferences.selected === id;
              return <button type="button" className="mux-skin-card" key={id} data-selected={!candidate && previewId === id} aria-pressed={!candidate && previewId === id} aria-label={`${text("预览", "Preview")} ${name}`} disabled={busy} onClick={() => inspect(id)}>
                <span className="mux-card-art" data-skin-animate="false">{isActive && <span className="mux-card-check" title={text("使用中", "Active")}><Checkmark16Regular /></span>}<span className="mux-card-character">{renderCharacter(skin)}</span></span>
                <span className="mux-card-info"><strong>{name}</strong><small>{isActive ? text("正在使用", "Currently active") : skin ? text(`${(skin.manifest.layered ? 6 : Object.keys({ ...skin.manifest.states, ...skin.manifest.live2d?.motions }).length)} 种状态`, `${(skin.manifest.layered ? 6 : Object.keys({ ...skin.manifest.states, ...skin.manifest.live2d?.motions }).length)} states`) : text("默认形象", "Original")}</small></span>
              </button>;
            })}
            <button type="button" className="mux-add-card" disabled={busy || !loaded} onClick={() => fileInput.current?.click()}><span><Add20Regular /></span><strong>{text("添加新伙伴", "Add a companion")}</strong><small>.muxskin / ZIP</small></button>
          </div>
        </section>
        <section className="mux-creator-guide"><span className="mux-guide-icon"><Image20Regular /></span><div><h3>{text("一张图片，也能成为你的伙伴", "One image. Your own companion.")}</h3><p>{text("透明 PNG 即可开始；更多表情与动画，交给你的创意。", "Start with a transparent PNG. Add expressions and animation when inspiration strikes.")}</p><div className="mux-resource-links"><a href="/skins/mux-layered.muxskin" download onClick={event => { event.preventDefault(); if (!busy) void action(() => resource("mux-layered.muxskin")); }}>{text("下载分层动态示例 ↗", "Layered example ↗")}</a><a href="/skins/mux-starter.muxskin" download onClick={event => { event.preventDefault(); if (!busy) void action(() => resource("mux-starter.muxskin")); }}>{text("下载示例包", "Starter pack")} ↗</a><a href="/skins/SKIN_SPEC.md" download onClick={event => { event.preventDefault(); if (!busy) void action(() => resource("SKIN_SPEC.md")); }}>{text("查看创作规范", "Creator guide")} ↗</a></div></div></section>
        <p className="mux-storage-note">{text("皮肤与设置仅保存在本机。导出皮肤包，就能备份或分享。", "Skins and settings stay on this device. Export a pack to back it up or share it.")}</p>
      </div>
    </div>
  </div>;
}
