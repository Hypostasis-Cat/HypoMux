import {
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
  Dropdown,
  Input,
  Option,
  Slider,
  Switch,
  Tab,
  TabList,
  Toast,
  ToastBody,
  ToastTitle,
  useId,
  useToastController,
} from "@fluentui/react-components";
import {
  ArrowSync20Regular,
  Delete20Regular,
  FolderOpen20Regular,
  Image20Regular,
  Save20Regular,
} from "@fluentui/react-icons";
import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { AppToaster } from "../components/AppToaster";
import { desktopPlatform } from "../platform/desktop";
import { appServices, type AdapterView, type CompleteAppSettings, type ConfigMigrationStatus } from "../platform/services";
import { SettingsSaveQueue, type SaveOutcome } from "../platform/settingsQueue";
import { adapterListKey } from "../state/adapterRuntime";
import { accentColours } from "../theme/appearance.presets";
import { useAppearance } from "../theme/appearance.store";
import { backgroundService } from "../theme/background.service";
import type { AccentPreset, AppearanceMode, MotionMode, PanelMaterial, WindowMaterial } from "../theme/appearance.types";
import { useI18n } from "../i18n/i18n";

const emptySettings: CompleteAppSettings = {
  mode: "tun",
  language: "zh",
  socks_port: 10800,
  http_port: 10801,
  weighted: false,
  strict_route: true,
  force_tun_connectivity_bypass: false,
  blocked_domain_bypass: false,
  blocked_domain_expiry: true,
  close_to_tray: false,
  autostart: false,
  auto_start_engine: false,
  dns_server: "223.5.5.5",
  dns_policy: "auto",
  dns_egress_mode: "auto",
  dns_adapter_id: "",
  selected_adapter_ids: [],
  adapter_weights: {},
  routing_rules: [],
};

type SettingRowA11y = { labelId: string; descriptionId: string };
const SettingRowA11yContext = createContext<SettingRowA11y | null>(null);

const useSettingRowA11y = () => useContext(SettingRowA11yContext) ?? undefined;

function SettingRow({
  title,
  description,
  children,
  danger = false,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
  danger?: boolean;
}) {
  const labelId = useId("setting-label");
  const descriptionId = useId("setting-description");
  return (
    <div
      className={`setting-row${danger ? " is-danger" : ""}`}
      role="group"
      aria-labelledby={labelId}
      aria-describedby={descriptionId}
    >
      <div className="setting-copy">
        <strong id={labelId}>{title}</strong>
        <span id={descriptionId} aria-live="polite">{description}</span>
      </div>
      <SettingRowA11yContext.Provider value={{ labelId, descriptionId }}>
        <div className="setting-control">{children}</div>
      </SettingRowA11yContext.Provider>
    </div>
  );
}

function SettingDropdown({
  value,
  options,
  disabled,
  onChange,
}: {
  value: string;
  options: Array<{ value: string; label: string }>;
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  const accessible = useSettingRowA11y();
  const selected = options.find((option) => option.value === value);
  return (
    <Dropdown
      className="settings-dropdown"
      value={selected?.label ?? value}
      selectedOptions={[value]}
      disabled={disabled}
      aria-labelledby={accessible?.labelId}
      aria-describedby={accessible?.descriptionId}
      onOptionSelect={(_, data) => data.optionValue && onChange(data.optionValue)}
    >
      {options.map((option) => (
        <Option key={option.value} value={option.value}>{option.label}</Option>
      ))}
    </Dropdown>
  );
}

function SettingSwitch({
  checked,
  disabled,
  onChange,
}: {
  checked: boolean;
  disabled?: boolean;
  onChange: (checked: boolean) => void;
}) {
  const accessible = useSettingRowA11y();
  return (
    <Switch
      checked={checked}
      disabled={disabled}
      aria-labelledby={accessible?.labelId}
      aria-describedby={accessible?.descriptionId}
      onChange={(_, data) => onChange(data.checked)}
    />
  );
}

function SettingSlider({
  min,
  max,
  value,
  disabled,
  valueText,
  onChange,
}: {
  min: number;
  max: number;
  value: number;
  disabled?: boolean;
  valueText: string;
  onChange: (value: number) => void;
}) {
  const accessible = useSettingRowA11y();
  return (
    <Slider
      min={min}
      max={max}
      value={value}
      disabled={disabled}
      aria-labelledby={accessible?.labelId}
      aria-describedby={accessible?.descriptionId}
      aria-valuetext={valueText}
      onChange={(_, data) => onChange(data.value)}
    />
  );
}

function SettingInput({
  value,
  placeholder,
  onChange,
}: {
  value: string;
  placeholder?: string;
  onChange: (value: string) => void;
}) {
  const accessible = useSettingRowA11y();
  return (
    <Input
      value={value}
      placeholder={placeholder}
      aria-labelledby={accessible?.labelId}
      aria-describedby={accessible?.descriptionId}
      onChange={(_, data) => onChange(data.value)}
    />
  );
}

function SettingTabs({
  selectedValue,
  onChange,
  children,
}: {
  selectedValue: string;
  onChange: (value: string) => void;
  children: React.ReactNode;
}) {
  const accessible = useSettingRowA11y();
  return (
    <TabList
      size="small"
      selectedValue={selectedValue}
      aria-labelledby={accessible?.labelId}
      aria-describedby={accessible?.descriptionId}
      onTabSelect={(_, data) => onChange(String(data.value))}
    >
      {children}
    </TabList>
  );
}

export function SettingsPage({
  adapterRuntime,
  onOpenBlockedDomains,
}: {
  adapterRuntime?: readonly AdapterView[];
  onOpenBlockedDomains: () => void;
}) {
  const [settings, setSettings] = useState<CompleteAppSettings>(emptySettings);
  const [adapters, setAdapters] = useState<AdapterView[]>([]);
  const [configPath, setConfigPath] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [wfpStatus, setWfpStatus] = useState("");
  const [migration, setMigration] = useState<ConfigMigrationStatus | null>(null);
  const [migrationDialog, setMigrationDialog] = useState<"migrate" | "rollback" | null>(null);
  const [migrationDialogOpen, setMigrationDialogOpen] = useState(false);
  const [sectionIndexFloating, setSectionIndexFloating] = useState(false);
  const { settings: appearance, update: updateAppearance, persistenceError: appearancePersistenceError } = useAppearance();
  const { locale, setLocale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const backgroundInput = useRef<HTMLInputElement>(null);
  const settingsPageRef = useRef<HTMLElement>(null);
  const sectionIndexSentinelRef = useRef<HTMLSpanElement>(null);
  const sectionIndexShellRef = useRef<HTMLDivElement>(null);
  const sectionIndexRef = useRef<HTMLElement>(null);
  const adapterRuntimeRef = useRef(adapterRuntime);
  const adapterRuntimeKeyRef = useRef<string>();
  adapterRuntimeRef.current = adapterRuntime;

  useEffect(() => {
    const root = settingsPageRef.current;
    const sentinel = sectionIndexSentinelRef.current;
    const shell = sectionIndexShellRef.current;
    const index = sectionIndexRef.current;
    if (!root || !sentinel || !shell || !index) return;

    const updateCenterShift = () => {
      const shift = Math.max(0, (shell.clientWidth - index.offsetWidth) / 2);
      index.style.setProperty("--hm-settings-index-center-shift", `${shift}px`);
    };

    updateCenterShift();
    const resizeObserver = new ResizeObserver(updateCenterShift);
    resizeObserver.observe(shell);
    resizeObserver.observe(index);

    const observer = new IntersectionObserver(([entry]) => {
      setSectionIndexFloating(!entry.isIntersecting && root.scrollTop > 0);
    }, {
      root,
      threshold: 0,
      rootMargin: "-18px 0px 0px 0px",
    });
    observer.observe(sentinel);
    return () => {
      observer.disconnect();
      resizeObserver.disconnect();
    };
  }, []);
  // Serialize settings persistence and track per-field ownership: concurrent
  // saves would otherwise let an earlier response overwrite a newer optimistic
  // value, and a failed operation's recovery must not overwrite a later
  // operation's success. Operations return SaveOutcome; the queue releases
  // their ownership and merges authoritative values (see settingsQueue).
  const saveQueue = useRef(new SettingsSaveQueue<CompleteAppSettings>()).current;

  useEffect(() => {
    saveQueue.attach((updater) => setSettings(updater));
  }, [saveQueue]);

  const enqueueSave = <T,>(operation: () => Promise<SaveOutcome<T, CompleteAppSettings>>, fields: string[] | null): Promise<T> =>
    saveQueue.enqueue(operation, fields).catch((error) => {
      // Errors are already surfaced via notify inside the operation; the
      // queue's rejection is only a control-flow signal. Swallow it here so
      // callers (React event handlers) never see an unhandled rejection.
      console.error("settings save failed:", error);
      return undefined as T;
    });

  const toasterId = useId("settings-toaster");
  const { dispatchToast } = useToastController(toasterId);

  const notify = useCallback((title: string, body: string, intent: "success" | "error" | "info" | "warning" = "success") => {
    dispatchToast(
      <Toast>
        <ToastTitle>{title}</ToastTitle>
        <ToastBody>{body}</ToastBody>
      </Toast>,
      { intent, timeout: 2800 },
    );
  }, [dispatchToast]);

  useEffect(() => {
    if (appearancePersistenceError) {
      notify(t("settings_background_image_save_failed"), appearancePersistenceError, "error");
    }
  }, [appearancePersistenceError, notify, t]);

  useEffect(() => {
    if (adapterRuntime === undefined) return;
    const nextKey = adapterListKey(adapterRuntime);
    if (adapterRuntimeKeyRef.current === nextKey) return;
    adapterRuntimeKeyRef.current = nextKey;
    setAdapters([...adapterRuntime]);
  }, [adapterRuntime]);

  useEffect(() => {
    Promise.all([
      appServices.settings.get(),
      appServices.settings.configPath(),
      appServices.settings.migrationStatus(),
      adapterRuntimeRef.current !== undefined
        ? Promise.resolve([...adapterRuntimeRef.current])
        : appServices.adapters.list().catch(() => []),
    ])
      .then(([loaded, path, migrationStatus, loadedAdapters]) => {
        setSettings({ ...emptySettings, ...loaded });
        setConfigPath(path);
        setMigration(migrationStatus);
        setAdapters(adapterRuntimeRef.current !== undefined ? [...adapterRuntimeRef.current] : loadedAdapters ?? []);
      })
      .catch((error) => notify(text("设置读取失败", "Failed to load settings"), String(error), "error"))
      .finally(() => setLoading(false));
  }, []);

  const save = (next: CompleteAppSettings, success?: string, fields: string[] | null = null): Promise<void> => {
    setSettings(next);
    return enqueueSave(async () => {
      setSaving(true);
      try {
        const persisted = await appServices.settings.update(next);
        setLocale(persisted.language);
        notify(t("infobar_success"), success ?? text("设置已保存", "Settings saved"));
        return { ok: true as const, value: undefined, authoritative: persisted };
      } catch (error) {
        const restored = await appServices.settings.get().catch(() => next);
        setLocale(restored.language);
        notify(text("保存失败", "Save failed"), String(error), "error");
        return {
          ok: false as const,
          error: error instanceof Error ? error : new Error(String(error)),
          restore: restored,
        };
      } finally {
        setSaving(false);
      }
    }, fields);
  };

  const patchAndSave = (patch: Partial<CompleteAppSettings>, success?: string) =>
    save({ ...settings, ...patch }, success, Object.keys(patch));

  const setAutostart = (enabled: boolean): Promise<void> => {
    // Optimistically mirror the backend semantics: disabling autostart also
    // clears auto_start_engine, so later full-replace payloads built from
    // this state can never revert the just-persisted autostart flag.
    setSettings((current) => ({
      ...current,
      autostart: enabled,
      auto_start_engine: enabled ? current.auto_start_engine : false,
    }));
    return enqueueSave(async () => {
      setSaving(true);
      try {
        const persisted = await appServices.settings.setAutostart(enabled);
        notify(
          enabled ? t("settings_autostart_on") : t("settings_autostart_off"),
          t("settings_autostart_hint"),
        );
        return { ok: true as const, value: undefined, authoritative: persisted };
      } catch (error) {
        const restored = await appServices.settings.get().catch(() => null);
        notify(t("settings_autostart_failed"), String(error), "error");
        return {
          ok: false as const,
          error: error instanceof Error ? error : new Error(String(error)),
          restore: restored,
        };
      } finally {
        setSaving(false);
      }
    }, ["autostart", "auto_start_engine"]);
  };

  const setAutoStartEngine = (enabled: boolean): Promise<void> => {
    setSettings((current) => ({ ...current, auto_start_engine: enabled }));
    return enqueueSave(async () => {
      setSaving(true);
      try {
        const persisted = await appServices.settings.setAutoStartEngine(enabled);
        notify(
          enabled ? t("settings_auto_start_engine_on") : t("settings_auto_start_engine_off"),
          t("settings_auto_start_engine_hint"),
        );
        return { ok: true as const, value: undefined, authoritative: persisted };
      } catch (error) {
        const restored = await appServices.settings.get().catch(() => null);
        notify(t("settings_auto_start_engine_failed"), String(error), "error");
        return {
          ok: false as const,
          error: error instanceof Error ? error : new Error(String(error)),
          restore: restored,
        };
      } finally {
        setSaving(false);
      }
    }, ["auto_start_engine"]);
  };

  const inspectWfp = async () => {
    setWfpStatus(text("正在检测网络组件与 TUN 兼容性…", "Checking network components and TUN compatibility…"));
    try {
      const result = await appServices.tun.preflight(settings.selected_adapter_ids ?? []);
      const issue = result.issues?.find((item) => item.code === "wfp_compatibility");
      if (result.wfp_ready) {
        setWfpStatus(text(
          "WFP 基础组件可用；严格路由将在 Core 启动时应用。",
          "WFP components are available; strict routing will be applied by Core on start.",
        ));
        notify(text("检测完成", "Check complete"), text("WFP 基础组件可用。", "WFP components are available."));
      } else {
        const detected = issue?.detail || result.wfp_detail || text(
          "WFP 只读检测未通过；修复需由独立高权限 Core 执行。",
          "The read-only WFP check failed; repair must be performed by the elevated Core.",
        );
        setWfpStatus(detected);
        const repaired = await appServices.engine.repairWfp();
        const verified = await appServices.tun.preflight(settings.selected_adapter_ids ?? []);
        if (repaired.engine_ready && verified.wfp_ready) {
          setWfpStatus(text(
            repaired.repaired
              ? "BFE 已由独立 Core 启动，WFP 复检通过。"
              : "独立 Core 已完成 WFP 复检，基础组件可用。",
            repaired.repaired
              ? "BFE was started by the isolated Core and WFP verification passed."
              : "The isolated Core verified that WFP components are available.",
          ));
          notify(
            text("修复完成", "Repair complete"),
            text("WFP/BFE 组件已通过复检。", "WFP/BFE components passed verification."),
          );
          return;
        }
        notify(
          text("检测到兼容性问题", "Compatibility issue detected"),
          text(
            "独立 Core 未能恢复 WFP；已保留严格路由偏好，普通权限 UI 未修改系统网络。",
            "The isolated Core could not restore WFP. The strict-routing preference was preserved and the standard UI made no system-network changes.",
          ),
          "warning",
        );
      }
    } catch (error) {
      setWfpStatus(String(error));
      notify(text("检测失败", "Check failed"), String(error), "error");
    }
  };

  const runMigrationAction = (): Promise<void> => {
    if (!migrationDialog) return Promise.resolve();
    // Route through the save queue: a migration must not interleave with a
    // queued full-replace save, or the migration result could be overwritten
    // by an older payload.
    return enqueueSave(async () => {
      setSaving(true);
      try {
        const next = migrationDialog === "migrate"
          ? await appServices.settings.migrateLegacy()
          : await appServices.settings.rollbackLegacy();
        setLocale(next.language);
        setMigration(await appServices.settings.migrationStatus());
        notify(
          migrationDialog === "migrate"
            ? text("旧版配置迁移完成", "Legacy settings migrated")
            : text("迁移结果已回滚", "Migration rolled back"),
          text(
            "旧版配置原文件始终保留，未执行删除。",
            "The original legacy configuration remains intact and was not deleted.",
          ),
        );
        setMigrationDialogOpen(false);
        return { ok: true as const, value: undefined, authoritative: next };
      } catch (error) {
        notify(text("配置迁移操作失败", "Configuration migration failed"), String(error), "error");
        return {
          ok: false as const,
          error: error instanceof Error ? error : new Error(String(error)),
          restore: null,
        };
      } finally {
        setSaving(false);
      }
    }, null);
  };

  return (
    <main ref={settingsPageRef} className="settings-page" aria-busy={loading || saving}>
      <AppToaster toasterId={toasterId} position="top-end" />
      <header className="page-heading">
        <div>
          <span className="section-kicker">{text("偏好设置", "HypoMux preferences")}</span>
          <h1>{t("settings_title")}</h1>
          <p>{text(
            "更改会写入当前用户配置；网络相关选项在引擎运行期间由独立 Core 应用。",
            "Changes are saved to the current user profile. Network options are applied by the independent Core while the engine runs.",
          )}</p>
        </div>
        <span key={loading ? "loading" : saving ? "saving" : "synced"} className="save-state motion-inline-swap" role="status" aria-live="polite">{loading
          ? text("正在读取…", "Loading…")
          : saving
            ? text("正在保存…", "Saving…")
            : text("配置已同步", "Settings synced")}</span>
      </header>

      <span ref={sectionIndexSentinelRef} className="settings-section-index-sentinel" aria-hidden="true" />
      <div ref={sectionIndexShellRef} className={`settings-section-index-shell${sectionIndexFloating ? " is-floating" : ""}`}>
        <nav ref={sectionIndexRef} className="settings-section-index" aria-label={text("设置分区", "Settings sections")}>
          {[
            ["settings-personalization", t("settings_personalization")],
            ["settings-global", t("settings_global")],
            ["settings-network", t("settings_network_dns")],
            ["settings-advanced", t("settings_advanced_network")],
            ["settings-config", t("settings_config_group")],
          ].map(([id, label]) => (
            <button key={id} type="button" onClick={() => document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" })}>
              {label}
            </button>
          ))}
        </nav>
      </div>

      <div className="settings-layout">
        <GlassSurface className="settings-section" id="settings-personalization">
          <h2>{t("settings_personalization")}</h2>
          <SettingRow title={t("settings_theme")} description={t("settings_theme_hint")}>
            <SettingTabs
              selectedValue={appearance.mode}
              onChange={(value) => updateAppearance({ mode: value as AppearanceMode })}
            >
              <Tab value="system">{t("settings_theme_auto")}</Tab>
              <Tab value="light">{t("settings_theme_light")}</Tab>
              <Tab value="dark">{t("settings_theme_dark")}</Tab>
            </SettingTabs>
          </SettingRow>
          <SettingRow
            title={text("窗口背景材质", "Window background material")}
            description={
              appearance.backgroundSource === "local"
                ? text(
                    "自定义背景图正在覆盖窗口材质；清除背景图后可切换 Mica 或纯色。",
                    "The custom background covers the window material. Clear it to switch between Mica and solid.",
                  )
                : text(
                    "默认浅色和深色使用稳定的纯色背景；如手动启用 Mica，将显示 Windows 11 原生窗口背景。卡片磨砂仅在使用自定义背景图时生效。",
                    "Light and dark use a stable solid background by default. Mica may be enabled manually. Card frosting only applies to custom backgrounds.",
                  )
            }
          >
            <SettingDropdown
              value={appearance.material}
              disabled={appearance.backgroundSource === "local"}
              options={[
                { value: "mica", label: "Mica" },
                { value: "solid", label: text("纯色", "Solid") },
              ]}
              onChange={(value) => updateAppearance({ material: value as WindowMaterial })}
            />
          </SettingRow>
          <SettingRow title={t("settings_theme_color")} description={t("settings_theme_color_hint")}>
            <div className="accent-row settings-accent-row">
              {(Object.keys(accentColours) as Exclude<AccentPreset, "custom">[]).map((name) => (
                <button
                  key={name}
                  className={`accent-swatch${appearance.accentPreset === name ? " is-active" : ""}`}
                  style={{ "--swatch": accentColours[name] } as React.CSSProperties}
                  aria-label={`${t("settings_theme_color")} ${name}`}
                  onClick={() => updateAppearance({ accentPreset: name })}
                />
              ))}
              <input
                className="accent-input"
                type="color"
                value={appearance.customAccent}
                aria-label={t("settings_theme_color_custom")}
                onChange={(event) => updateAppearance({ customAccent: event.target.value, accentPreset: "custom" })}
              />
            </div>
          </SettingRow>
          <SettingRow
            title={text("界面动效", "Interface motion")}
            description={text(
              "控制页面切换、侧栏滑块与控件过渡；仅使用 HypoMux 的设置，不跟随 Windows 动效选项。",
              "Controls page transitions, the navigation slider, and control animations. Uses only the HypoMux setting.",
            )}
          >
            <SettingDropdown
              value={appearance.motion}
              options={[
                { value: "standard", label: text("完整动效", "Full motion") },
                { value: "reduced", label: text("精简动效", "Reduced motion") },
                { value: "off", label: text("关闭动效", "Off") },
              ]}
              onChange={(value) => updateAppearance({ motion: value as MotionMode })}
            />
          </SettingRow>
          <SettingRow title={t("settings_background_image")} description={t("settings_background_image_hint")}>
            <div className="background-actions">
              <Button icon={<Image20Regular />} onClick={() => backgroundInput.current?.click()}>{t("settings_background_image_choose")}</Button>
              <Button
                appearance="subtle"
                icon={<Delete20Regular />}
                disabled={!appearance.localBackgroundUrl}
                onClick={() => {
                  backgroundService.release(appearance.localBackgroundUrl);
                  updateAppearance({
                    localBackgroundUrl: undefined,
                    backgroundSource: "system",
                    material: "mica",
                    presetId: "windows-mica",
                  });
                }}
              >
                {t("settings_background_image_clear")}
              </Button>
              <input
                ref={backgroundInput}
                className="visually-hidden"
                type="file"
                aria-label={t("settings_background_image_choose")}
                accept=".png,.jpg,.jpeg,.bmp,.webp,image/png,image/jpeg,image/bmp,image/webp"
                onChange={async (event) => {
                  const file = event.target.files?.[0];
                  if (!file) return;
                  try {
                    const dataURL = await backgroundService.fromFile(file);
                  updateAppearance({
                    localBackgroundUrl: dataURL,
                    backgroundSource: "local",
                  });
                  } catch (error) {
                    notify(t("settings_background_image_invalid"), String(error), "error");
                  } finally {
                    event.target.value = "";
                  }
                }}
              />
            </div>
          </SettingRow>
          <SettingRow
            title={text("卡片材质", "Card material")}
            description={text(
              "仅在使用自定义背景图时生效，决定内容卡片使用高斯磨砂还是纯色。",
              "Only available with a custom background; chooses frosted or solid cards.",
            )}
          >
            <SettingDropdown
              value={appearance.panelMaterial}
              disabled={appearance.backgroundSource !== "local"}
              options={[
                { value: "blur", label: text("高斯磨砂", "Gaussian frost") },
                { value: "solid", label: text("纯色卡片", "Solid cards") },
              ]}
              onChange={(value) => updateAppearance({ panelMaterial: value as PanelMaterial })}
            />
          </SettingRow>
          <SettingRow
            title={text("磨砂强度", "Frost strength")}
            description={text(
              "仅在自定义背景和高斯磨砂卡片下生效；数值越高，背景细节越柔和。",
              "Only applies to frosted cards over a custom background. Higher values soften more detail.",
            )}
          >
            <div className="slider-value">
              <SettingSlider
                min={0}
                max={40}
                value={appearance.panelBlur}
                valueText={`${appearance.panelBlur}px`}
                disabled={appearance.backgroundSource !== "local" || appearance.panelMaterial !== "blur"}
                onChange={(value) => updateAppearance({ panelBlur: value })}
              />
              <span>{appearance.panelBlur}px</span>
            </div>
          </SettingRow>
          <SettingRow title={t("settings_content_card_opacity")} description={t("settings_content_card_opacity_hint")}>
            <div className="slider-value">
              <SettingSlider
                min={0}
                max={100}
                value={appearance.panelOpacity}
                valueText={`${appearance.panelOpacity}%`}
                disabled={appearance.backgroundSource !== "local"}
                onChange={(value) => updateAppearance({ panelOpacity: value })}
              />
              <span>{appearance.panelOpacity}%</span>
            </div>
          </SettingRow>
        </GlassSurface>

        <GlassSurface className="settings-section" id="settings-global">
          <h2>{t("settings_global")}</h2>
          <SettingRow title={t("settings_language")} description={text("保存界面语言偏好", "Save the interface language preference")}>
            <SettingDropdown
              value={settings.language}
              disabled={loading || saving}
              options={[
                { value: "zh", label: t("settings_language_zh") },
                { value: "en", label: t("settings_language_en") },
              ]}
              onChange={(value) => {
                const nextLocale = value as "zh" | "en";
                setLocale(nextLocale);
                void patchAndSave({ language: nextLocale }, t("settings_lang_saved"));
              }}
            />
          </SettingRow>
          <SettingRow title={t("settings_close_behavior")} description={text(
            "关闭主窗口时隐藏到托盘，或直接退出并恢复运行状态",
            "Hide the main window to the tray, or exit and restore the active network state.",
          )}>
            <SettingDropdown
              value={settings.close_to_tray ? "tray" : "exit"}
              disabled={loading || saving}
              options={[
                { value: "tray", label: t("settings_close_to_tray") },
                { value: "exit", label: t("settings_close_to_exit") },
              ]}
              onChange={(value) => patchAndSave({ close_to_tray: value === "tray" })}
            />
          </SettingRow>
          <SettingRow title={t("settings_proxy_port")} description={text(
            "SOCKS5 与 HTTP/HTTPS 监听端口，范围 1–65534",
            "SOCKS5 and HTTP/HTTPS listening ports, range 1–65534.",
          )}>
            <div className="port-controls">
              <label>SOCKS5 <Input type="number" min={1} max={65534} value={String(settings.socks_port)} onChange={(_, data) => setSettings((current) => ({ ...current, socks_port: Number(data.value) }))} /></label>
              <label>HTTP <Input type="number" min={1} max={65534} value={String(settings.http_port)} onChange={(_, data) => setSettings((current) => ({ ...current, http_port: Number(data.value) }))} /></label>
            </div>
          </SettingRow>
        </GlassSurface>

        <GlassSurface className="settings-section" id="settings-network">
          <h2>{t("settings_network_dns")}</h2>
          <SettingRow title={t("settings_dns_server")} description={t("settings_dns_fallback_hint")}>
            <SettingInput
              value={settings.dns_server}
              placeholder={t("settings_dns_placeholder")}
              onChange={(value) => setSettings((current) => ({ ...current, dns_server: value }))}
            />
          </SettingRow>
          <SettingRow title={t("settings_doh_policy")} description={t("settings_doh_hint")}>
            <SettingDropdown
              value={settings.dns_policy}
              options={[
                { value: "auto", label: t("settings_doh_auto") },
                { value: "off", label: t("settings_doh_off") },
                { value: "alidns", label: t("settings_doh_alidns") },
                { value: "dnspod", label: t("settings_doh_dnspod") },
                { value: "google", label: "Google DNS" },
              ]}
              onChange={(value) => setSettings((current) => ({ ...current, dns_policy: value }))}
            />
          </SettingRow>
          <SettingRow title={t("settings_dns_egress")} description={t("settings_dns_egress_hint")}>
            <SettingDropdown
              value={settings.dns_egress_mode === "adapter" ? `adapter:${settings.dns_adapter_id ?? ""}` : settings.dns_egress_mode}
              disabled={loading || saving}
              options={[
                { value: "auto", label: t("settings_dns_egress_auto") },
                { value: "system", label: t("settings_dns_egress_system") },
                ...adapters
                  .filter((adapter) => adapter.selected && adapter.operational)
                  .map((adapter) => ({
                    value: `adapter:${adapter.id}`,
                    label: `${t("settings_dns_egress_adapter_prefix")} · ${adapter.name}`,
                  })),
              ]}
              onChange={(value) => setSettings((current) => value.startsWith("adapter:")
                ? { ...current, dns_egress_mode: "adapter", dns_adapter_id: value.slice("adapter:".length) }
                : { ...current, dns_egress_mode: value, dns_adapter_id: "" })}
            />
          </SettingRow>
          <div className="settings-actions">
            <Button appearance="primary" icon={<Save20Regular />} disabled={loading || saving} onClick={() => save(settings, text("端口与 DNS 设置已保存", "Proxy ports and DNS settings saved"))}>
              {text("保存端口与 DNS", "Save ports and DNS")}
            </Button>
          </div>
        </GlassSurface>

        <GlassSurface className="settings-section" id="settings-advanced">
          <h2>{t("settings_advanced_network")}</h2>
          <SettingRow
            title={t("settings_force_tun")}
            description={t("settings_force_tun_hint")}
            danger
          >
            <SettingSwitch checked={settings.force_tun_connectivity_bypass} onChange={(checked) => patchAndSave({ force_tun_connectivity_bypass: checked })} />
          </SettingRow>
          <SettingRow title={t("settings_wfp_strict_route")} description={t("settings_wfp_strict_route_hint")}>
            <SettingSwitch checked={settings.strict_route} onChange={(checked) => patchAndSave({ strict_route: checked })} />
          </SettingRow>
          <SettingRow title={t("settings_wfp_repair")} description={wfpStatus || t("settings_wfp_repair_unknown")}>
            <Button icon={<ArrowSync20Regular />} onClick={inspectWfp}>{t("settings_wfp_repair_button")}</Button>
          </SettingRow>
          <SettingRow title={t("blocked_enable")} description={t("blocked_enable_hint")}>
            <SettingSwitch checked={settings.blocked_domain_bypass} onChange={(checked) => patchAndSave({ blocked_domain_bypass: checked })} />
          </SettingRow>
          <SettingRow title={t("blocked_expiry_toggle")} description={t("blocked_expiry_hint")}>
            <SettingSwitch checked={settings.blocked_domain_expiry} onChange={(checked) => patchAndSave({ blocked_domain_expiry: checked })} />
          </SettingRow>
          <SettingRow title={t("settings_blocked_domains_manage")} description={t("settings_blocked_domains_manage_hint")}>
            <Button onClick={onOpenBlockedDomains}>{t("settings_blocked_domains_open")}</Button>
          </SettingRow>
        </GlassSurface>

        <GlassSurface className="settings-section" id="settings-config">
          <h2>{t("settings_config_group")}</h2>
          <SettingRow title={t("settings_autostart")} description={t("settings_autostart_hint")}>
            <SettingSwitch checked={settings.autostart} disabled={saving} onChange={(checked) => setAutostart(checked)} />
          </SettingRow>
          <SettingRow title={t("settings_auto_start_engine")} description={t("settings_auto_start_engine_hint")}>
            <SettingSwitch
              checked={settings.auto_start_engine}
              disabled={saving || !settings.autostart}
              onChange={(checked) => setAutoStartEngine(checked)}
            />
          </SettingRow>
          <SettingRow title={t("settings_config_path")} description={configPath || text("正在读取配置文件位置…", "Reading configuration path…")}>
            <Button
              icon={<FolderOpen20Regular />}
              disabled={!configPath}
              onClick={() => desktopPlatform.openDirectory(configPath.replace(/[\\/][^\\/]+$/, ""))}
            >
              {text("打开目录", "Open folder")}
            </Button>
          </SettingRow>
          <SettingRow
            title={text("旧版配置迁移与回滚", "Legacy configuration migration and rollback")}
            description={migration?.message || text("未检测到 HypoMux v2.x 配置", "No HypoMux v2.x configuration was found")}
          >
            <div className="migration-actions">
              <Button
                disabled={!migration?.legacy_found || saving}
                onClick={() => {
                  setMigrationDialog("migrate");
                  setMigrationDialogOpen(true);
                }}
              >
                {text("迁移旧版配置", "Migrate legacy settings")}
              </Button>
              <Button
                appearance="subtle"
                disabled={!migration?.applied || saving}
                onClick={() => {
                  setMigrationDialog("rollback");
                  setMigrationDialogOpen(true);
                }}
              >
                {text("回滚", "Rollback")}
              </Button>
            </div>
          </SettingRow>
        </GlassSurface>
      </div>

      <Dialog open={migrationDialogOpen} onOpenChange={(_, data) => !data.open && setMigrationDialogOpen(false)}>
        <DialogSurface>
          <DialogBody>
            <DialogTitle>{migrationDialog === "migrate"
              ? text("迁移 HypoMux v2.x 配置？", "Migrate HypoMux v2.x settings?")
              : text("回滚旧配置迁移？", "Roll back the legacy migration?")}</DialogTitle>
            <DialogContent>
              {migrationDialog === "migrate"
                ? text(
                  "将导入网卡选择、端口、运行模式、DNS、DoH、分流规则、WFP 偏好和域名隔离设置。当前新版配置会先备份，旧版文件不会被修改或删除。",
                  "Adapter selection, ports, run mode, DNS, DoH, split rules, WFP preferences, and domain-isolation settings will be imported. Current settings are backed up first; legacy files are never modified or deleted.",
                )
                : text(
                  "将恢复迁移前的新版配置；若迁移前没有新版配置，则恢复默认值。旧版文件不会被修改或删除。",
                  "Settings from before migration will be restored. If no new configuration existed, defaults are restored. Legacy files are never modified or deleted.",
                )}
            </DialogContent>
            <DialogActions>
              <DialogTrigger disableButtonEnhancement><Button>{t("routing_dialog_cancel")}</Button></DialogTrigger>
              <Button appearance="primary" disabled={saving} onClick={runMigrationAction}>
                {migrationDialog === "migrate"
                  ? text("确认迁移", "Confirm migration")
                  : text("确认回滚", "Confirm rollback")}
              </Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </main>
  );
}
