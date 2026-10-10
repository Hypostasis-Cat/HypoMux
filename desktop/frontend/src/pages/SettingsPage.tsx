import { usePageActive } from "../components/shell/PageActivity";
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
  useId,
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
import { savePreferences, useSkins } from "../components/ai/skins/store";
import { useAppNotifications } from "../components/notifications/AppNotifications";
import { desktopPlatform } from "../platform/desktop";
import { appServices, type AdapterView, type CompleteAppSettings, type ConfigMigrationStatus } from "../platform/services";
import { SettingsSaveQueue, type SaveOutcome } from "../platform/settingsQueue";
import { adapterListKey } from "../state/adapterRuntime";
import { SYSTEM_PROXY_TAKEOVER_EVENT } from "../state/systemProxyTakeover";
import { ADAPTER_VISIBILITY_EVENT } from "../state/adapterVisibility";
import { publishAIAvailability } from "../state/aiAvailability";
import { accentColours } from "../theme/appearance.presets";
import { useAppearance } from "../theme/appearance.store";
import { backgroundService } from "../theme/background.service";
import { backgroundPresets, builtinBackgrounds, builtinBackgroundSizes } from "../theme/wallpaper";
import type { AccentPreset, AppearanceMode, MotionMode, PanelMaterial, WindowMaterial } from "../theme/appearance.types";
import { useI18n } from "../i18n/i18n";
import { SettingsAddressList } from "./SettingsAddressList";
import { SettingsNavigation, settingsCategories, useSettingsCategory } from "./SettingsNavigation";
import { validDNSAddress, validDoHAddress, validDoTAddress } from "./dnsSettings";

const emptySettings: CompleteAppSettings = {
  ai_enabled: true,
  steam_cdn_enabled: false,
  mode: "tun",
  language: "zh",
  update_channel: "stable",
  socks_port: 10800,
  http_port: 10801,
  system_proxy_takeover: true,
  weighted: false,
  strict_route: true,
  tun_stack: "system",
  force_tun_connectivity_bypass: false,
  blocked_domain_bypass: false,
  blocked_domain_expiry: true,
  close_to_tray: false,
  hide_virtual_adapters: true,
  autostart: false,
  auto_start_engine: false,
  auto_connect_wifi: false,
  dns_server: "223.5.5.5",
  dns_policy: "auto",
  dns_egress_mode: "auto",
  dns_adapter_id: "",
  selected_adapter_ids: [],
  adapter_weights: {},
  routing_rules: [],
};

function SettingGroup({ title, children }: { title: string; children: React.ReactNode }) {
  const headingId = useId("settings-group");
  return <section className="settings-group" aria-labelledby={headingId}>
    <h2 className="settings-group-heading" id={headingId}>{title}</h2>
    <GlassSurface className="settings-section">{children}</GlassSurface>
  </section>;
}

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
      className="settings-theme-options"
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
  // Manual network edits must never leak into auto-saved preference updates.
  const pageActive = usePageActive();
  const settingsRevision = useRef(0);
  const [networkDraft, setNetworkDraft] = useState<Partial<Pick<CompleteAppSettings, "socks_port" | "http_port" | "dns_server" | "dns_servers" | "doh_servers" | "dot_servers" | "dns_policy" | "dns_egress_mode" | "dns_adapter_id">>>({});
  const [networkValidationAttempted, setNetworkValidationAttempted] = useState(false);
  const networkSettings = { ...settings, ...networkDraft };
  const dnsServers = networkSettings.dns_servers ?? [networkSettings.dns_server];
  const dohServers = networkSettings.doh_servers ?? [];
  const dotServers = networkSettings.dot_servers ?? [];
  const networkDirty = Object.entries(networkDraft).some(([key, value]) => JSON.stringify(settings[key as keyof CompleteAppSettings]) !== JSON.stringify(value));
  useEffect(() => {
    if (!networkDirty) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [networkDirty]);
  const [savingCompanion, setSavingCompanion] = useState(false);
  const [adapters, setAdapters] = useState<AdapterView[]>([]);
  const [configPath, setConfigPath] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const { preferences: companionPreferences, loaded: companionLoaded } = useSkins(!loading && !loadFailed && settings.ai_enabled !== false);
  const [loadRevision, setLoadRevision] = useState(0);
  const [saving, setSaving] = useState(false);
  const [wfpStatus, setWfpStatus] = useState("");
  const [migration, setMigration] = useState<ConfigMigrationStatus | null>(null);
  const [migrationDialog, setMigrationDialog] = useState<"migrate" | "rollback" | null>(null);
  const [migrationDialogOpen, setMigrationDialogOpen] = useState(false);
  const [category, selectCategory] = useSettingsCategory();
  const { settings: appearance, update: updateAppearance, persistenceError: appearancePersistenceError } = useAppearance();
  const { locale, setLocale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const dnsErrors = dnsServers.map(value => validDNSAddress(value) ? "" : text("请输入有效的单播 IPv4 或 IPv6 地址。", "Enter a valid unicast IPv4 or IPv6 address."));
  const dohErrors = dohServers.map(value => validDoHAddress(value) ? "" : text("请输入完整 HTTPS 地址，如 https://dns.example.com/dns-query。", "Enter a full HTTPS URL, e.g. https://dns.example.com/dns-query."));
  const customDoHMissing = networkSettings.dns_policy === "custom" && dohServers.length === 0;
  const dotErrors = dotServers.map(value => validDoTAddress(value) ? "" : text("请输入 tls://域名[:端口]，如 tls://dns.alidns.com。", "Enter tls://hostname[:port], e.g. tls://dns.alidns.com."));
  const customDoTMissing = networkSettings.dns_policy === "dot" && dotServers.length === 0;
  const backgroundInput = useRef<HTMLInputElement>(null);
  const settingsPageRef = useRef<HTMLElement>(null);
  const adapterRuntimeRef = useRef(adapterRuntime);
  const adapterRuntimeKeyRef = useRef<string>();
  adapterRuntimeRef.current = adapterRuntime;

  useEffect(() => {
    settingsPageRef.current?.scrollTo?.({ top: 0 });
  }, [category]);
  // Serialize settings persistence and track per-field ownership: concurrent
  // saves would otherwise let an earlier response overwrite a newer optimistic
  // value, and a failed operation's recovery must not overwrite a later
  // operation's success. Operations return SaveOutcome; the queue releases
  // their ownership and merges authoritative values (see settingsQueue).
  const saveQueue = useRef(new SettingsSaveQueue<CompleteAppSettings>()).current;

  useEffect(() => {
    saveQueue.attach((updater) => setSettings(updater));
  }, [saveQueue]);

  useEffect(() => {
    window.dispatchEvent(new CustomEvent(SYSTEM_PROXY_TAKEOVER_EVENT, {
      detail: settings.system_proxy_takeover,
    }));
  }, [settings.system_proxy_takeover]);

  useEffect(() => {
    window.dispatchEvent(new CustomEvent(ADAPTER_VISIBILITY_EVENT, {
      detail: settings.hide_virtual_adapters ?? true,
    }));
  }, [settings.hide_virtual_adapters]);

  const enqueueSave = <T,>(operation: () => Promise<SaveOutcome<T, CompleteAppSettings>>, fields: string[] | null): Promise<T> =>
    (settingsRevision.current++, saveQueue.enqueue(operation, fields)).catch((error) => {
      // Errors are already surfaced via notify inside the operation; the
      // queue's rejection is only a control-flow signal. Swallow it here so
      // callers (React event handlers) never see an unhandled rejection.
      console.error("settings save failed:", error);
      return undefined as T;
    });

  const { notify: pushNotification } = useAppNotifications();

  const notify = useCallback((title: string, body: string, intent: "success" | "error" | "info" | "warning" = "success") => {
    pushNotification({ title, message: body, intent, dedupeKey: `settings:${intent}:${title}` });
  }, [pushNotification]);

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
    if (!pageActive) return;
    const revision = settingsRevision.current;
    let cancelled = false;
    setLoading(true);
    setLoadFailed(false);
    Promise.all([
      appServices.settings.get(),
      appServices.settings.configPath(),
      appServices.settings.migrationStatus(),
      adapterRuntimeRef.current !== undefined
        ? Promise.resolve([...adapterRuntimeRef.current])
        : appServices.adapters.list().catch(() => []),
    ])
      .then(([loaded, path, migrationStatus, loadedAdapters]) => {
        if (cancelled) return;
        if (revision === settingsRevision.current) {
          setSettings(current => saveQueue.mergeAuthoritative({ ...emptySettings, ...loaded }, current));
          publishAIAvailability(loaded.ai_enabled !== false);
        }
        setConfigPath(path);
        setMigration(migrationStatus);
        setAdapters(adapterRuntimeRef.current !== undefined ? [...adapterRuntimeRef.current] : loadedAdapters ?? []);
      })
      .catch((error) => {
        if (cancelled) return;
        setLoadFailed(true);
        notify(text("设置读取失败", "Failed to load settings"), String(error), "error");
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [loadRevision, pageActive, saveQueue]);

  useEffect(() => {
    const changed = () => setLoadRevision(value => value + 1);
    window.addEventListener("hypomux:ai-changed", changed);
    return () => window.removeEventListener("hypomux:ai-changed", changed);
  }, []);

  const save = (next: CompleteAppSettings, success: string | undefined, fields: string[]): Promise<boolean> => {
    setSettings(next);
    return enqueueSave(async () => {
      setSaving(true);
      try {
        const persisted = await appServices.settings.update(next, fields);
        publishAIAvailability(persisted.ai_enabled !== false);
        setLocale(persisted.language);
        notify(t("infobar_success"), success ?? text("设置已保存", "Settings saved"));
        return { ok: true as const, value: true, authoritative: persisted };
      } catch (error) {
        const restored = await appServices.settings.get().catch(() => settings);
        publishAIAvailability(restored.ai_enabled !== false);
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
    }, fields).then(Boolean);
  };

  const patchAndSave = (patch: Partial<CompleteAppSettings>, success?: string) =>
    save({ ...settings, ...patch }, success, Object.keys(patch));

  const saveNetwork = async () => {
    setNetworkValidationAttempted(true);
    if (dnsErrors.some(Boolean) || dohErrors.some(Boolean) || dotErrors.some(Boolean) || customDoHMissing || customDoTMissing) {
      requestAnimationFrame(() => {
        const root = settingsPageRef.current;
        const invalid = root?.querySelector<HTMLInputElement>('[aria-invalid="true"] input, input[aria-invalid="true"]')
          ?? root?.querySelector<HTMLButtonElement>(`${customDoTMissing ? ".settings-custom-dot" : ".settings-custom-doh"} .settings-address-add`);
        invalid?.focus();
        const errorEntry = invalid?.closest('.settings-address-entry')
          ?? root?.querySelector(`${customDoTMissing ? ".settings-custom-dot" : ".settings-custom-doh"} .settings-resolver-note`);
        errorEntry?.scrollIntoView?.({ block: "nearest", inline: "nearest", behavior: "instant" });
      });
      return;
    }
    const submitted = networkDraft;
    if (await save({ ...settings, ...submitted }, text("端口与 DNS 设置已保存，重启聚合后生效", "Proxy ports and DNS settings saved; restart aggregation to apply"), Object.keys(submitted))) {
      setNetworkDraft(current => Object.fromEntries(Object.entries(current).filter(([key, value]) => value !== submitted[key as keyof typeof submitted])));
      setNetworkValidationAttempted(false);
    }
  };

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
        publishAIAvailability(next.ai_enabled !== false);
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

  const activeCategory = settingsCategories(text).find(item => item.id === category)!;

  return (
    <main ref={settingsPageRef} className="settings-page" aria-busy={loading || saving}>
      <header className="page-heading">
        <div>
          <h1>{t("settings_title")}</h1>
          <p>{text(
            "调整应用偏好，让 HypoMux 更合你的习惯。",
            "Make HypoMux work the way you like.",
          )}</p>
        </div>
        <div className="settings-save-feedback">
          <span key={loading ? "loading" : loadFailed ? "error" : saving ? "saving" : networkDirty ? "dirty" : "synced"} className="save-state motion-inline-swap" data-error={loadFailed || undefined} role="status" aria-live="polite">{loading
            ? text("正在读取…", "Loading…")
            : loadFailed
              ? text("配置未读取", "Settings unavailable")
              : saving
                ? text("正在保存…", "Saving…")
                : networkDirty ? text("端口与 DNS 有未保存的更改", "Unsaved port and DNS changes") : text("配置已同步", "Settings synced")}</span>
          {networkDirty && category !== "network" && <Button size="small" appearance="subtle" onClick={() => selectCategory("network")}>{text("去保存", "Review changes")}</Button>}
          {category === "network" && <Button appearance="primary" icon={<Save20Regular />} disabled={loading || loadFailed || saving || !networkDirty} onClick={() => void saveNetwork()}>
            {saving ? text("正在保存…", "Saving…") : text("保存端口与 DNS", "Save ports and DNS")}
          </Button>}
          {loadFailed && <Button size="small" appearance="subtle" icon={<ArrowSync20Regular />} onClick={() => setLoadRevision(value => value + 1)}>{text("重试", "Retry")}</Button>}
        </div>
      </header>

      <div className="settings-workspace">
        <SettingsNavigation category={category} onSelect={selectCategory} networkDirty={networkDirty} text={text} />
        <div className="settings-category-body">
          <div className="settings-category-panel" role="tabpanel" id="settings-category-panel"
            aria-labelledby={`settings-tab-${category}`} tabIndex={0}>
            <div className="settings-category-content" key={category}>
            <header className="settings-category-heading">
              <p>{category === "network"
                  ? text("端口与 DNS 保存后重启聚合生效；系统代理开关即时保存。", "Save ports and DNS, then restart aggregation. The system proxy switch saves immediately.")
                  : activeCategory.description}</p>
              <span className="settings-persistence-label">{category === "network" ? text("手动保存", "Manual save") : category === "configuration" ? text("配置维护", "Maintenance") : text("自动保存", "Auto save")}</span>
            </header>
            {category === "general" && <>
              <SettingGroup title={text("应用偏好", "App preferences")}>
                <SettingRow title={t("settings_language")} description={text("保存界面语言偏好", "Save the interface language preference")}>
                  <SettingDropdown
                    value={settings.language}
                    disabled={loading || loadFailed || saving}
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
                    disabled={loading || loadFailed || saving}
                    options={[
                      { value: "tray", label: t("settings_close_to_tray") },
                      { value: "exit", label: t("settings_close_to_exit") },
                    ]}
                    onChange={(value) => patchAndSave({ close_to_tray: value === "tray" })}
                  />
                </SettingRow>
                <SettingRow title={text("首页隐藏虚拟网卡", "Hide virtual adapters on Home")} description={text(
                  "默认隐藏 VMware、Hyper-V 等虚拟网卡。关闭后显示全部网卡；已有网卡选择保持不变。",
                  "Hide virtual adapters such as VMware and Hyper-V by default. Turn off to show all adapters. Existing selections are preserved.",
                )}>
                  <SettingSwitch checked={settings.hide_virtual_adapters ?? true} disabled={loading || loadFailed || saving}
                    onChange={(checked) => patchAndSave({ hide_virtual_adapters: checked })} />
                </SettingRow>
                <SettingRow title={text("更新渠道", "Update channel")} description={text(
                  "正式版适合日常使用；预览版包含 Beta / RC，可能不稳定。切回正式版不会自动降级。保存后可在「关于」中检查更新。",
                  "Stable is recommended for everyday use. Preview includes Beta / RC and may be unstable. Switching to Stable does not downgrade. Check for updates in About after saving.",
                )}>
                  <SettingDropdown
                    value={settings.update_channel ?? "stable"}
                    disabled={loading || saving || loadFailed}
                    options={[
                      { value: "stable", label: text("正式版", "Stable") },
                      { value: "preview", label: text("预览版（Beta / RC）", "Preview (Beta / RC)") },
                    ]}
                    onChange={(value) => patchAndSave({ update_channel: value as "stable" | "preview" })}
                  />
                </SettingRow>
              </SettingGroup>
              <SettingGroup title={text("开机与启动", "Startup")}>
                <SettingRow title={t("settings_autostart")} description={t("settings_autostart_hint")}>
                  <SettingSwitch checked={settings.autostart} disabled={loading || loadFailed || saving} onChange={(checked) => setAutostart(checked)} />
                </SettingRow>
                <SettingRow title={t("settings_auto_start_engine")} description={t("settings_auto_start_engine_hint")}>
                  <SettingSwitch
                    checked={settings.auto_start_engine}
                    disabled={loading || loadFailed || saving || !settings.autostart}
                    onChange={(checked) => setAutoStartEngine(checked)}
                  />
                </SettingRow>
                <SettingRow title={text("开机自动连接 Wi-Fi", "Connect Wi-Fi at startup")} description={text(
                  "自动加速前，为已选无线网卡连接 Windows 中已保存且允许自动连接的网络，最多等待 2 分钟。关闭后停止主动连接，已连接的 Wi-Fi 保持连接。",
                  "Before automatic acceleration, connect selected Wi-Fi adapters using saved Windows networks that allow automatic connection. Wait up to 2 minutes. Turning this off stops connection requests and keeps existing connections.",
                )}>
                  <SettingSwitch
                    checked={settings.auto_connect_wifi ?? false}
                    disabled={loading || loadFailed || saving || !settings.autostart || !settings.auto_start_engine}
                    onChange={(checked) => patchAndSave({ auto_connect_wifi: checked })}
                  />
                </SettingRow>
              </SettingGroup>
            </>}
            {category === "appearance" && <>
              <SettingGroup title={text("主题与颜色", "Theme and color")}>
                <SettingRow title={t("settings_theme")} description={t("settings_theme_hint")}>
                  <SettingTabs
                    selectedValue={appearance.mode}
                    onChange={(value) => updateAppearance({ mode: value as AppearanceMode })}
                  >
                    {(["system", "light", "dark"] as const).map(mode => <Tab key={mode} value={mode}
                      className="settings-theme-option" aria-label={t(mode === "system" ? "settings_theme_auto" : mode === "light" ? "settings_theme_light" : "settings_theme_dark")}>
                      <span className={`settings-theme-preview is-${mode}`} aria-hidden="true"><i /><span><i /><i /><i /></span></span>
                      <span>{t(mode === "system" ? "settings_theme_auto" : mode === "light" ? "settings_theme_light" : "settings_theme_dark")}</span>
                    </Tab>)}
                  </SettingTabs>
                </SettingRow>
                <SettingRow title={t("settings_theme_color")} description={t("settings_theme_color_hint")}>
                  <div className="accent-row settings-accent-row">
                    {(Object.keys(accentColours) as Exclude<AccentPreset, "custom">[]).map((name) => (
                      <button
                        key={name}
                        className={`accent-swatch${appearance.accentPreset === name ? " is-active" : ""}`}
                        style={{ "--swatch": accentColours[name] } as React.CSSProperties}
                        aria-label={`${t("settings_theme_color")} ${name}`}
                        aria-pressed={appearance.accentPreset === name}
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
              </SettingGroup>
              <SettingGroup title={text("背景", "Background")}>
                <SettingRow title={text("主题背景", "Theme background")} description={text("选择默认背景、内置预设或自己的图片，点击即刻应用。", "Choose the default, a preset, or your own image. Changes apply immediately.")}>
                  <div className="background-preset-picker">
                    <div className="background-preset-grid">
                      <button type="button" className="background-preset-card"
                        aria-pressed={appearance.backgroundSource === "system"}
                        onClick={() => updateAppearance({ backgroundSource: "system" })}>
                        <span className="background-preset-art background-preset-default" aria-hidden="true" />
                        <strong>{text("默认背景", "Default background")}</strong>
                        <small>{text("简洁原生 · 跟随主题", "Native simplicity · follows theme")}</small>
                      </button>
                      {backgroundPresets.map((preset) => (
                        <button type="button" key={preset.id} className="background-preset-card"
                          aria-pressed={appearance.backgroundSource === "builtin" && appearance.builtinBackground === preset.id}
                          onClick={() => updateAppearance({
                            backgroundSource: "builtin", builtinBackground: preset.id,
                            ...(preset.id === "soft-bloom" ? { panelOpacity: 72 } : {}),
                          })}>
                          <span className="background-preset-art" aria-hidden="true" style={{ background: builtinBackgrounds[preset.id], backgroundSize: builtinBackgroundSizes[preset.id] }} />
                          <strong>{text(preset.name, preset.english)}</strong>
                          <small>{text(preset.description, preset.englishDescription)}</small>
                        </button>
                      ))}
                      <div className="background-custom-tile">
                      <button type="button" className="background-preset-card"
                        aria-pressed={appearance.backgroundSource === "local"}
                        onClick={() => appearance.localBackgroundUrl
                          ? updateAppearance({ backgroundSource: "local" })
                          : backgroundInput.current?.click()}>
                        <span className="background-preset-art background-preset-custom" aria-hidden="true"
                          style={appearance.localBackgroundUrl ? { backgroundImage: `url("${appearance.localBackgroundUrl}")` } : undefined}>
                          {!appearance.localBackgroundUrl && <Image20Regular />}
                        </span>
                        <strong>{text("自定义图片", "Custom image")}</strong>
                        <small>{text("上传喜欢的图片作为背景", "Use a picture of your own")}</small>
                      </button>
                    {appearance.localBackgroundUrl && <div className="background-custom-actions">
                      <Button size="small" icon={<Image20Regular />} aria-label={text("更换图片", "Replace image")} title={text("更换图片", "Replace image")} onClick={() => backgroundInput.current?.click()} />
                      <Button size="small" icon={<Delete20Regular />} aria-label={text("移除图片", "Remove image")} title={text("移除图片", "Remove image")} onClick={() => {
                        backgroundService.release(appearance.localBackgroundUrl);
                        updateAppearance({ localBackgroundUrl: undefined, ...(appearance.backgroundSource === "local" ? { backgroundSource: "system" as const } : {}) });
                      }} />
                    </div>}
                      </div>
                    </div>
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
              </SettingGroup>
              <SettingGroup title={text("材质与动效", "Materials and motion")}>
                <SettingRow
                  title={text("窗口背景材质", "Window background material")}
                  description={text("Mica 柔化标题栏与侧栏的背景；纯色保留主题底色，关闭磨砂。", "Mica softens the title bar and sidebar background. Solid keeps the theme colors without frosting.")}
                >
                  <SettingDropdown
                    value={appearance.material}
                    options={[
                      { value: "mica", label: "Mica" },
                      { value: "solid", label: text("纯色", "Solid") },
                    ]}
                    onChange={(value) => updateAppearance({ material: value as WindowMaterial })}
                  />
                </SettingRow>
                <SettingRow
                  title={text("界面动效", "Interface motion")}
                  description={text(
                    "控制页面与控件的过渡，自动遵循系统的减少动态效果设置。",
                    "Page and control transitions respect your system’s reduced motion setting.",
                  )}
                >
                  <SettingDropdown
                    value={appearance.motion}
                    options={[
                      { value: "standard", label: text("完整动效", "Full motion") },
                      { value: "reduced", label: text("适中动画", "Moderate motion") },
                      { value: "off", label: text("关闭动效", "Off") },
                    ]}
                    onChange={(value) => updateAppearance({ motion: value as MotionMode })}
                  />
                </SettingRow>
                <SettingRow
                  title={text("卡片材质", "Card material")}
                  description={text(
                    "适用于背景预设和自定义图片，选择高斯磨砂或清晰卡片。",
                    "Choose frosted or clear cards over a preset or custom image.",
                  )}
                >
                  <SettingDropdown
                    value={appearance.panelMaterial}
                    disabled={appearance.backgroundSource !== "local" && appearance.backgroundSource !== "builtin"}
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
                    "使用高斯磨砂时，数值越高，卡片后的背景越柔和。",
                    "With frosted cards, higher values soften the background behind them.",
                  )}
                >
                  <div className="slider-value">
                    <SettingSlider
                      min={0}
                      max={40}
                      value={appearance.panelBlur}
                      valueText={`${appearance.panelBlur}px`}
                      disabled={(appearance.backgroundSource !== "local" && appearance.backgroundSource !== "builtin") || appearance.panelMaterial !== "blur"}
                      onChange={(value) => updateAppearance({ panelBlur: value })}
                    />
                    <span>{appearance.panelBlur}px</span>
                  </div>
                </SettingRow>
                <SettingRow title={t("settings_content_card_opacity")} description={text("适用于预设和自定义背景；提高不透明度可增强文字可读性。", "Available with presets and custom backgrounds. Higher opacity improves text readability.")}>
                  <div className="slider-value">
                    <SettingSlider
                      min={0}
                      max={100}
                      value={appearance.panelOpacity}
                      valueText={`${appearance.panelOpacity}%`}
                      disabled={appearance.backgroundSource !== "local" && appearance.backgroundSource !== "builtin"}
                      onChange={(value) => updateAppearance({ panelOpacity: value })}
                    />
                    <span>{appearance.panelOpacity}%</span>
                  </div>
                </SettingRow>
              </SettingGroup>
            </>}
            {category === "network" && <>
              <SettingGroup title={text("DNS 解析", "DNS resolution")}>
                <SettingRow title={text("DNS 解析策略", "DNS resolution policy")} description={networkSettings.dns_policy === "dot"
                  ? text("使用下方 DoT 列表，校验服务器证书，失败时不回退明文 DNS。", "Use the DoT list below with certificate verification and no plaintext DNS fallback.")
                  : networkSettings.dns_policy === "custom"
                    ? text("使用下方 DoH 列表，失败时不回退 DNS。", "Use the DoH list below without DNS fallback.")
                  : networkSettings.dns_policy === "off"
                    ? text("使用下方 DNS 列表。", "Use the DNS list below.")
                    : networkSettings.dns_policy === "auto"
                      ? text("优先 DoH，失败后回退 DNS；未添加 DoH 时使用内置服务。", "Try DoH first, then DNS. An empty DoH list uses built-in providers.")
                      : text("使用所选内置 DoH 服务。", "Use the selected built-in DoH provider.")}>
                  <SettingDropdown
                    disabled={loading || loadFailed || saving}
                    value={networkSettings.dns_policy}
                    options={[
                      { value: "auto", label: t("settings_doh_auto") },
                      { value: "off", label: t("settings_doh_off") },
                      { value: "alidns", label: t("settings_doh_alidns") },
                      { value: "dnspod", label: t("settings_doh_dnspod") },
                      { value: "google", label: "Google DNS" },
                      { value: "custom", label: text("仅使用自定义 DoH", "Custom DoH only") },
                      { value: "dot", label: text("仅使用自定义 DoT", "Custom DoT only") },
                    ]}
                    onChange={(value) => setNetworkDraft((current) => ({ ...current, dns_policy: value }))}
                  />
                </SettingRow>
                <div className="settings-resolver-grid">
                  <section className="settings-resolver-card" aria-labelledby="settings-dns-list-title">
                    <div className="settings-resolver-heading">
                      <h3 id="settings-dns-list-title">DNS</h3>
                      <span>{text("IPv4 / IPv6 地址", "IPv4 / IPv6 addresses")}</span>
                    </div>
                    <SettingsAddressList
                      disabled={loading || loadFailed || saving} values={dnsServers} label="DNS" minimum={1}
                      addLabel={text("添加 DNS", "Add DNS")} removeLabel={text("删除 DNS", "Remove DNS")}
                      placeholder="223.5.5.5" errors={networkValidationAttempted ? dnsErrors : []}
                      onChange={values => setNetworkDraft(current => ({ ...current, dns_server: values[0], dns_servers: values }))}
                    />
                    <div className="settings-resolver-note settings-address-hint">{["custom", "dot"].includes(networkSettings.dns_policy)
                      ? text("仅用于解析加密 DNS 服务的域名；业务域名仍使用加密查询。", "Only resolves encrypted DNS server hostnames; application domains still use encrypted queries.")
                      : text("按列表顺序尝试。", "Tried in list order.")}</div>
                  </section>
                  <section className="settings-resolver-card settings-custom-doh" aria-labelledby="settings-doh-list-title">
                    <div className="settings-resolver-heading">
                      <h3 id="settings-doh-list-title">DoH</h3>
                      <span>{text("自定义 HTTPS 地址", "Custom HTTPS URLs")}</span>
                    </div>
                    <SettingsAddressList disabled={loading || loadFailed || saving} values={dohServers} label="DoH" type="url"
                      addLabel={text("添加 DoH", "Add DoH")} removeLabel={text("删除 DoH", "Remove DoH")}
                      placeholder="https://dns.example.com/dns-query"
                      emptyHint={text("暂无自定义 DoH", "No custom DoH servers")}
                      errors={networkValidationAttempted ? dohErrors : []}
                      onChange={values => setNetworkDraft(current => ({ ...current, doh_servers: values }))} />
                    <div className="settings-resolver-note">
                      {networkValidationAttempted && customDoHMissing
                        ? <span className="settings-address-error" aria-live="polite">{text("请添加 DoH 地址，或切换解析策略。", "Add a DoH URL or change the policy.")}</span>
                        : <span className="settings-address-hint">{!["auto", "custom"].includes(networkSettings.dns_policy)
                          ? text("当前策略不使用此列表。", "This list is inactive with the current policy.")
                          : text("支持自定义端口、路径和查询参数。", "Supports custom ports, paths, and query parameters.")}</span>}
                    </div>
                  </section>
                  <section className="settings-resolver-card settings-custom-dot" aria-labelledby="settings-dot-list-title">
                    <div className="settings-resolver-heading">
                      <h3 id="settings-dot-list-title">DoT</h3>
                      <span>{text("自定义 TLS 地址", "Custom TLS URLs")}</span>
                    </div>
                    <SettingsAddressList disabled={loading || loadFailed || saving} values={dotServers} label="DoT" type="url"
                      addLabel={text("添加 DoT", "Add DoT")} removeLabel={text("删除 DoT", "Remove DoT")}
                      placeholder="tls://dns.alidns.com"
                      emptyHint={text("暂无自定义 DoT", "No custom DoT servers")}
                      errors={networkValidationAttempted ? dotErrors : []}
                      onChange={values => setNetworkDraft(current => ({ ...current, dot_servers: values }))} />
                    <div className="settings-resolver-note">
                      {networkValidationAttempted && customDoTMissing
                        ? <span className="settings-address-error" aria-live="polite">{text("请添加 DoT 地址，或切换解析策略。", "Add a DoT URL or change the policy.")}</span>
                        : <span className="settings-address-hint">{networkSettings.dns_policy !== "dot"
                          ? text("选择“仅使用自定义 DoT”后生效。", "Select Custom DoT only to use this list.")
                          : text("默认端口 853；支持域名或 IPv4 / IPv6 地址及自定义端口。", "Default port 853; supports hostnames, IPv4 / IPv6 addresses, and custom ports.")}</span>}
                    </div>
                  </section>
                </div>
                <SettingRow title={t("settings_dns_egress")} description={t("settings_dns_egress_hint")}>
                  <SettingDropdown
                    value={networkSettings.dns_egress_mode === "adapter" ? `adapter:${networkSettings.dns_adapter_id ?? ""}` : networkSettings.dns_egress_mode}
                    disabled={loading || loadFailed || saving}
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
                    onChange={(value) => setNetworkDraft((current) => value.startsWith("adapter:")
                      ? { ...current, dns_egress_mode: "adapter", dns_adapter_id: value.slice("adapter:".length) }
                      : { ...current, dns_egress_mode: value, dns_adapter_id: "" })}
                  />
                </SettingRow>
              </SettingGroup>
              <SettingGroup title={text("本地代理", "Local proxy")}>
                <SettingRow title={t("settings_proxy_port")} description={text(
                  "SOCKS5 与 HTTP/HTTPS 监听端口，范围 1–65534",
                  "SOCKS5 and HTTP/HTTPS listening ports, range 1–65534.",
                )}>
                  <div className="port-controls">
                    <label>SOCKS5 <Input autoComplete="off" disabled={loading || loadFailed} type="number" min={1} max={65534} name="socks_port" value={String(networkSettings.socks_port)} onChange={(_, data) => setNetworkDraft((current) => ({ ...current, socks_port: Number(data.value) }))} /></label>
                    <label>HTTP <Input autoComplete="off" disabled={loading || loadFailed} type="number" min={1} max={65534} name="http_port" value={String(networkSettings.http_port)} onChange={(_, data) => setNetworkDraft((current) => ({ ...current, http_port: Number(data.value) }))} /></label>
                  </div>
                </SettingRow>
                <SettingRow title={t("settings_system_proxy_takeover")} description={t("settings_system_proxy_takeover_hint")}>
                  <SettingSwitch
                    checked={settings.system_proxy_takeover}
                    disabled={loading || loadFailed || saving}
                    onChange={(checked) => void patchAndSave(
                      { system_proxy_takeover: checked },
                      checked
                        ? t("settings_system_proxy_takeover_on")
                        : t("settings_system_proxy_takeover_off"),
                    )}
                  />
                </SettingRow>
              </SettingGroup>
            </>}
            {category === "ai" && <>
              <SettingGroup title={text("AI 与小 Mux", "AI and Mux")}>
                <SettingRow title={text("启用 AI 功能", "Enable AI features")}
                  description={text("关闭 AI 对话、工具操作和外部 AI 连接，隐藏侧栏入口与小 Mux。模型配置与历史保留，已执行的修改不会撤销。", "Turn off AI chat and tool actions, disconnect external AI access, and hide the AI sidebar entry and Mux. Model settings and history are kept; completed changes are not undone.")}>
                  <SettingSwitch checked={settings.ai_enabled !== false} disabled={loading || loadFailed || saving}
                    onChange={enabled => void patchAndSave({ ai_enabled: enabled })} />
                </SettingRow>
                {!loading && !loadFailed && settings.ai_enabled !== false && <SettingRow title={text("显示 AI 小精灵", "Show AI companion")}
                  description={text("在页面角落显示小精灵。隐藏后仍可从侧栏打开 AI 助手。", "Show the companion in the corner. You can still open AI Assistant from the sidebar when hidden.")}>
                  <SettingSwitch checked={companionPreferences.visible !== false} disabled={!companionLoaded || savingCompanion}
                    onChange={(visible) => {
                      setSavingCompanion(true);
                      void savePreferences({ visible }).catch((error) => {
                        notify(text("无法保存小精灵设置", "Unable to save companion preference"), String(error), "error");
                      }).finally(() => setSavingCompanion(false));
                    }} />
                </SettingRow>}
                {!loading && !loadFailed && settings.ai_enabled !== false && <SettingRow title={text("模型与连接", "Models and connections")}
                  description={text("管理 AI 服务、模型和连接方式。", "Manage AI providers, models and connections.")}>
                  <Button onClick={() => window.dispatchEvent(new Event("hypomux:ai-settings"))}>{text("AI 助手设置", "AI assistant settings")}</Button>
                </SettingRow>}
              </SettingGroup>
            </>}
            {category === "advanced" && <>
              <SettingGroup title={text("TUN 与兼容性", "TUN and compatibility")}>
                <SettingRow
                  title={text("TUN 协议栈", "TUN stack")}
                  description={text(
                    "System 使用系统协议栈；Mixed 使用系统 TCP + gVisor UDP；gVisor 使用完整用户态协议栈。遇到兼容性问题时可切换尝试，保存后下次启动 TUN 生效。",
                    "System uses the OS stack; Mixed uses system TCP + gVisor UDP; gVisor uses a full userspace stack. Try another stack for compatibility issues. Applies the next time TUN starts.",
                  )}
                >
                  <SettingDropdown
                    value={settings.tun_stack || "system"}
                    disabled={loading || loadFailed || saving}
                    options={[
                      { value: "system", label: text("System（默认）", "System (default)") },
                      { value: "mixed", label: text("Mixed（混合）", "Mixed (hybrid)") },
                      { value: "gvisor", label: text("gVisor（用户态）", "gVisor (userspace)") },
                    ]}
                    onChange={(value) => void patchAndSave(
                      { tun_stack: value },
                      text("TUN 协议栈已保存，下次启动 TUN 生效", "TUN stack saved; applies the next time TUN starts"),
                    )}
                  />
                </SettingRow>
                <SettingRow
                  title={t("settings_force_tun")}
                  description={t("settings_force_tun_hint")}
                  danger
                >
                  <SettingSwitch checked={settings.force_tun_connectivity_bypass} disabled={loading || loadFailed || saving} onChange={(checked) => patchAndSave({ force_tun_connectivity_bypass: checked })} />
                </SettingRow>
                <SettingRow title={t("settings_wfp_strict_route")} description={t("settings_wfp_strict_route_hint")}>
                  <SettingSwitch checked={settings.strict_route} disabled={loading || loadFailed || saving} onChange={(checked) => patchAndSave({ strict_route: checked })} />
                </SettingRow>
                <SettingRow title={t("settings_wfp_repair")} description={wfpStatus || t("settings_wfp_repair_unknown")}>
                  <Button icon={<ArrowSync20Regular />} disabled={loading || loadFailed || saving} onClick={inspectWfp}>{t("settings_wfp_repair_button")}</Button>
                </SettingRow>
              </SettingGroup>
              <SettingGroup title={text("域名分流", "Domain routing")}>
                <SettingRow title={t("blocked_enable")} description={t("blocked_enable_hint")}>
                  <SettingSwitch checked={settings.blocked_domain_bypass} disabled={loading || loadFailed || saving} onChange={(checked) => patchAndSave({ blocked_domain_bypass: checked })} />
                </SettingRow>
                <SettingRow title={t("blocked_expiry_toggle")} description={t("blocked_expiry_hint")}>
                  <SettingSwitch checked={settings.blocked_domain_expiry} disabled={loading || loadFailed || saving} onChange={(checked) => patchAndSave({ blocked_domain_expiry: checked })} />
                </SettingRow>
                <SettingRow title={t("settings_blocked_domains_manage")} description={t("settings_blocked_domains_manage_hint")}>
                  <Button onClick={onOpenBlockedDomains}>{t("settings_blocked_domains_open")}</Button>
                </SettingRow>
              </SettingGroup>
              <SettingGroup title={text("缓存", "Cache")}>
                <SettingRow
                  title={text("FakeIP 与规则集缓存", "FakeIP and rule-set cache")}
                  description={text(
                    "已自动启用持久化缓存，保留 FakeIP 映射；远程规则集接入后也可复用。缓存位于配置目录下的 cache/sing-box.db，不随 TUN 重启清除。",
                    "Persistent caching is enabled automatically for FakeIP mappings and future remote rule sets. Stored at cache/sing-box.db inside the configuration directory and retained across TUN restarts.",
                  )}
                >
                  <span>{text("已启用", "Enabled")}</span>
                </SettingRow>
              </SettingGroup>
            </>}
            {category === "configuration" && <>
              <SettingGroup title={text("配置管理", "Configuration management")}>
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
              </SettingGroup>
            </>}
            </div>
          </div>
        </div>
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
