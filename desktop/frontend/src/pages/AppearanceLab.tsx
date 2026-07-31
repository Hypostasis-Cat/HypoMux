import {
  Badge,
  Button,
  Select,
  Slider,
  Tab,
  TabList,
  Toast,
  Toaster,
  ToastTitle,
  useId,
  useToastController,
} from "@fluentui/react-components";
import { ArrowReset20Regular, Image20Regular } from "@fluentui/react-icons";
import { useRef } from "react";
import { AppearancePreview } from "../components/appearance/AppearancePreview";
import { appearancePresets, accentColours } from "../theme/appearance.presets";
import { useAppearance } from "../theme/appearance.store";
import { backgroundService } from "../theme/background.service";
import type {
  AccentPreset,
  AppearanceMode,
  InterfaceDensity,
  MotionMode,
  WindowMaterial,
} from "../theme/appearance.types";

function RangeControl({
  label,
  value,
  min,
  max,
  suffix = "",
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  suffix?: string;
  onChange: (value: number) => void;
}) {
  return (
    <label className="range-control">
      <span>{label}<strong>{value}{suffix}</strong></span>
      <Slider min={min} max={max} value={value} onChange={(_, data) => onChange(data.value)} />
    </label>
  );
}

export function AppearanceLab() {
  const { settings, nativeResult, update, applyPreset, reset } = useAppearance();
  const fileInput = useRef<HTMLInputElement>(null);
  const toasterId = useId("appearance-lab-toaster");
  const { dispatchToast } = useToastController(toasterId);

  const showToast = () => {
    dispatchToast(
      <Toast><ToastTitle>主题令牌已应用到 Fluent UI 控件</ToastTitle></Toast>,
      { intent: "success", timeout: 2200 },
    );
  };

  return (
    <main className="appearance-lab">
      <Toaster toasterId={toasterId} position="top-end" />
      <aside className="appearance-controls glass-surface" data-tone="secondary">
        <div className="lab-heading">
          <div>
            <span className="section-kicker">仅开发环境</span>
            <h1>Appearance Lab</h1>
          </div>
          <Button appearance="subtle" icon={<ArrowReset20Regular />} aria-label="重置外观" onClick={reset} />
        </div>

        <div className="preset-grid">
          {appearancePresets.map((preset) => (
            <button
              key={preset.id}
              className={`preset-button${settings.presetId === preset.id ? " is-active" : ""}`}
              onClick={() => applyPreset(preset.id)}
            >
              <strong>{preset.name}</strong>
              <span>{preset.description}</span>
            </button>
          ))}
        </div>

        <section className="lab-control-section">
          <h2>模式与材质</h2>
          <TabList
            selectedValue={settings.mode}
            onTabSelect={(_, data) => update({ mode: data.value as AppearanceMode })}
            size="small"
          >
            <Tab value="system">系统</Tab>
            <Tab value="light">浅色</Tab>
            <Tab value="dark">深色</Tab>
          </TabList>
          <label className="field-control">
            <span>窗口材质</span>
            <Select
              value={settings.material}
              onChange={(_, data) => update({ material: data.value as WindowMaterial, presetId: settings.presetId })}
            >
              <option value="mica">Mica</option>
              <option value="solid">Solid</option>
            </Select>
          </label>
          <Badge appearance="outline" color={nativeResult.fallback ? "warning" : "success"}>
            {nativeResult.fallback ? "Web 材质回退" : "Windows 原生材质已应用"}
          </Badge>
        </section>

        <section className="lab-control-section">
          <h2>强调色与背景</h2>
          <div className="accent-row">
            {(Object.keys(accentColours) as Exclude<AccentPreset, "custom">[]).map((name) => (
              <button
                key={name}
                className={`accent-swatch${settings.accentPreset === name ? " is-active" : ""}`}
                style={{ "--swatch": accentColours[name] } as React.CSSProperties}
                aria-label={`强调色 ${name}`}
                onClick={() => update({ accentPreset: name })}
              />
            ))}
            <input
              className="accent-input"
              type="color"
              value={settings.customAccent}
              aria-label="自定义强调色"
              onChange={(event) => update({ customAccent: event.target.value, accentPreset: "custom" })}
            />
          </div>
          <div className="background-actions">
            <Button appearance="secondary" icon={<Image20Regular />} onClick={() => fileInput.current?.click()}>
              选择本地背景
            </Button>
            <input
              ref={fileInput}
              className="visually-hidden"
              type="file"
              accept="image/*"
              onChange={async (event) => {
                const file = event.target.files?.[0];
                if (!file) return;
                backgroundService.release(settings.localBackgroundUrl);
                update({
                  localBackgroundUrl: await backgroundService.fromFile(file),
                  backgroundSource: "local",
                  material: "mica",
                });
              }}
            />
            <Select
              value={settings.backgroundSource}
              aria-label="背景来源"
              onChange={(_, data) => update({ backgroundSource: data.value as typeof settings.backgroundSource })}
            >
              <option value="system">系统材质</option>
              <option value="builtin">内置背景</option>
              <option value="local">本地图片</option>
              <option value="gradient">渐变</option>
              <option value="solid">纯色</option>
            </Select>
          </div>
          <RangeControl label="背景遮罩" value={settings.backgroundOverlay} min={0} max={80} suffix="%" onChange={(value) => update({ backgroundOverlay: value })} />
        </section>

        <section className="lab-control-section">
          <h2>玻璃表面</h2>
          <RangeControl label="不透明度" value={settings.panelOpacity} min={45} max={96} suffix="%" onChange={(value) => update({ panelOpacity: value })} />
          <RangeControl label="模糊" value={settings.panelBlur} min={0} max={40} suffix="px" onChange={(value) => update({ panelBlur: value })} />
          <RangeControl label="饱和度" value={settings.panelSaturation} min={80} max={160} suffix="%" onChange={(value) => update({ panelSaturation: value })} />
          <RangeControl label="圆角" value={settings.radius} min={4} max={18} suffix="px" onChange={(value) => update({ radius: value })} />
        </section>

        <section className="lab-control-section two-column-controls">
          <label className="field-control">
            <span>界面密度</span>
            <Select value={settings.density} onChange={(_, data) => update({ density: data.value as InterfaceDensity })}>
              <option value="compact">紧凑</option>
              <option value="standard">标准</option>
              <option value="comfortable">宽松</option>
            </Select>
          </label>
          <label className="field-control">
            <span>动画</span>
            <Select value={settings.motion} onChange={(_, data) => update({ motion: data.value as MotionMode })}>
              <option value="off">关闭</option>
              <option value="reduced">减少</option>
              <option value="standard">标准</option>
            </Select>
          </label>
        </section>
      </aside>

      <section className="lab-stage">
        <div className="lab-stage-heading">
          <div>
            <span className="section-kicker">实时主题预览</span>
            <h2>三层材质与控件状态</h2>
          </div>
          <span>所有调节立即生效</span>
        </div>
        <AppearancePreview onToast={showToast} />
      </section>
    </main>
  );
}
