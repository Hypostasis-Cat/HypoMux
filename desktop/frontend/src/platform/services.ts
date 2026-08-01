import * as AdapterService from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/services/adapterservice";
import * as DiagnosticsService from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/services/diagnosticsservice";
import * as EngineService from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/services/engineservice";
import * as RoutingRuleService from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/services/routingruleservice";
import * as SettingsService from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/services/settingsservice";
import * as TunService from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/services/tunservice";
import { Call } from "@wailsio/runtime";
import type {
  AppSettings,
  AdapterView,
  DiagnosticResult,
  DiagnosticSnapshot,
  EngineSnapshot,
  RoutingRule,
  RoutingSnapshot,
  RoutingValidation,
  SupportLogSession,
  SupportLogSnapshot,
  TunPreflightIssue,
  TunPreflightSnapshot,
} from "../../bindings/github.com/Hypostasis-Cat/HypoMux/desktop/internal/services/models";

export type {
  AdapterView,
  DiagnosticResult,
  DiagnosticSnapshot,
  EngineSnapshot,
  RoutingRule,
  RoutingSnapshot,
  RoutingValidation,
  SupportLogSession,
  SupportLogSnapshot,
  TunPreflightIssue,
  TunPreflightSnapshot,
};

export type CompleteAppSettings = AppSettings & {
  language: "zh" | "en";
  force_tun_connectivity_bypass: boolean;
  blocked_domain_bypass: boolean;
  blocked_domain_expiry: boolean;
  autostart: boolean;
  auto_start_engine: boolean;
};

export type BlockedDomainEntry = {
  adapter: string;
  domain: string;
  expires_at: string;
  remaining_seconds: number;
  permanent: boolean;
};

export type BlockedDomainSnapshot = {
  enabled: boolean;
  use_expiry: boolean;
  entries: BlockedDomainEntry[];
};

export type ReleaseInfo = {
  tag_name: string;
  name: string;
  notes: string;
  page_url: string;
  installer_url: string;
  installer_name: string;
  installer_size: number;
  installer_digest?: string;
};

export type UpdateCheckResult = {
  current_version: string;
  available: boolean;
  release: ReleaseInfo;
};

export type UpdateProgress = {
  state: "idle" | "starting" | "downloading" | "ready" | "installing" | "failed";
  downloaded: number;
  total: number;
  message?: string;
};

export type WFPRepairResult = {
  elevated: boolean;
  bfe_running: boolean;
  engine_ready: boolean;
  repair_attempted: boolean;
  repaired: boolean;
  detail?: string;
};

export type ConnectionView = {
  id: number;
  process?: string;
  protocol: string;
  client?: string;
  target?: string;
  domain?: string;
  remote_ip?: string;
  remote_port?: string;
  adapter?: string;
  outbound: string;
  outbound_detail?: string;
  started_at: string;
  bytes_up: number;
  bytes_down: number;
};

export type ConnectionListSnapshot = {
  phase: string;
  sampled_at: string;
  connections: ConnectionView[];
};

export type ConfigMigrationStatus = {
  legacy_found: boolean;
  applied: boolean;
  legacy_path: string;
  backup_path?: string;
  message: string;
};

const settingsMethod = (method: string) =>
  `github.com/Hypostasis-Cat/HypoMux/desktop/internal/services.SettingsService.${method}`;
const blockedDomainMethod = (method: string) =>
  `github.com/Hypostasis-Cat/HypoMux/desktop/internal/services.BlockedDomainService.${method}`;
const updaterMethod = (method: string) =>
  `github.com/Hypostasis-Cat/HypoMux/desktop/internal/services.UpdaterService.${method}`;
const engineMethod = (method: string) =>
  `github.com/Hypostasis-Cat/HypoMux/desktop/internal/services.EngineService.${method}`;
const appearanceMethod = (method: string) =>
  `github.com/Hypostasis-Cat/HypoMux/desktop/internal/services.AppearanceService.${method}`;

export async function withServiceTimeout<T>(
  request: Promise<T>,
  timeoutMs: number,
  operation: string,
): Promise<T> {
  let timer: number | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = window.setTimeout(() => {
      reject(new Error(`${operation} (${Math.ceil(timeoutMs / 1000)}s)`));
    }, timeoutMs);
  });
  try {
    return await Promise.race([request, timeout]);
  } finally {
    if (timer !== undefined) window.clearTimeout(timer);
  }
}

// Pages never import generated Wails bindings directly. This facade keeps the
// desktop transport replaceable and gives browser-only visual QA an explicit,
// visibly disconnected fixture rather than pretending a real core is running.
export const appServices = {
  adapters: {
    list: () => AdapterService.List(),
    refresh: () => AdapterService.Refresh(),
    save: (mode: string, weighted: boolean, adapters: AdapterView[]) =>
      AdapterService.SaveSelection(mode, weighted, adapters),
  },
  engine: {
    snapshot: () => EngineService.Snapshot(),
    connections: () =>
      Call.ByName(engineMethod("Connections")) as Promise<ConnectionListSnapshot>,
    start: (mode: string) => EngineService.Start(mode),
    stop: () => EngineService.Stop(),
    repairWfp: () =>
      Call.ByName(
        "github.com/Hypostasis-Cat/HypoMux/desktop/internal/services.EngineService.RepairWFP",
      ) as Promise<WFPRepairResult>,
  },
  routing: {
    snapshot: () => RoutingRuleService.Snapshot(),
    validate: (rule: RoutingRule, existing: RoutingRule[]) =>
      RoutingRuleService.Validate(rule, existing),
    save: (rules: RoutingRule[]) => RoutingRuleService.Save(rules),
    listProcesses: () => RoutingRuleService.ListProcesses(),
    importRules: () => RoutingRuleService.Import(),
    exportRules: (rules: RoutingRule[]) => RoutingRuleService.Export(rules),
  },
  diagnostics: {
    latest: () => DiagnosticsService.Latest(),
    run: (adapterIDs: string[]) => DiagnosticsService.Run(adapterIDs),
    cancel: () => DiagnosticsService.Cancel(),
    logs: () => DiagnosticsService.Logs(),
    exportLogs: () => DiagnosticsService.ExportLogs(),
    openLogDirectory: () => DiagnosticsService.OpenLogDirectory(),
  },
  tun: {
    latest: () => TunService.Latest(),
    preflight: (adapterIDs: string[]) => TunService.Preflight(adapterIDs),
  },
  settings: {
    get: async () => (await SettingsService.Get()) as CompleteAppSettings,
    update: (settings: CompleteAppSettings) =>
      Call.ByName(settingsMethod("Update"), settings) as Promise<CompleteAppSettings>,
    setAutostart: (enabled: boolean) =>
      Call.ByName(settingsMethod("SetAutostart"), enabled) as Promise<CompleteAppSettings>,
    setAutoStartEngine: (enabled: boolean) =>
      Call.ByName(settingsMethod("SetAutoStartEngine"), enabled) as Promise<CompleteAppSettings>,
    configPath: () => Call.ByName(settingsMethod("ConfigPath")) as Promise<string>,
    migrationStatus: () =>
      Call.ByName(settingsMethod("MigrationStatus")) as Promise<ConfigMigrationStatus>,
    migrateLegacy: () =>
      Call.ByName(settingsMethod("MigrateLegacy")) as Promise<CompleteAppSettings>,
    rollbackLegacy: () =>
      Call.ByName(settingsMethod("RollbackLegacyMigration")) as Promise<CompleteAppSettings>,
  },
  appearance: {
    load: () => Call.ByName(appearanceMethod("Load")) as Promise<string>,
    save: (payload: string) => Call.ByName(appearanceMethod("Save"), payload) as Promise<string>,
  },
  blockedDomains: {
    list: () => Call.ByName(blockedDomainMethod("List")) as Promise<BlockedDomainSnapshot>,
    remove: (adapter: string, domain: string) =>
      Call.ByName(blockedDomainMethod("Remove"), adapter, domain) as Promise<void>,
    clear: () => Call.ByName(blockedDomainMethod("Clear")) as Promise<void>,
  },
  updater: {
    check: () => Call.ByName(updaterMethod("Check")) as Promise<UpdateCheckResult>,
    download: (release: ReleaseInfo) =>
      Call.ByName(updaterMethod("Download"), release) as Promise<string>,
    launchInstaller: (path: string) =>
      Call.ByName(updaterMethod("LaunchInstaller"), path) as Promise<void>,
    progress: () => Call.ByName(updaterMethod("Progress")) as Promise<UpdateProgress>,
  },
};
