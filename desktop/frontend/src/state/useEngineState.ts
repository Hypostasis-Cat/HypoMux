import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  appServices,
  type AdapterView,
  type DiagnosticResult,
  type EngineSnapshot,
  type TunPreflightSnapshot,
  withServiceTimeout,
} from "../platform/services";
import { desktopPlatform } from "../platform/desktop";
import { isDesktopRuntime } from "../platform/runtime";
import { adapterSaveInput, adapterSaveQueue } from "../platform/adapterSaveQueue";
import { startSerialPoll } from "../platform/serialPoll";
import { adapterListKey } from "./adapterRuntime";

export type EnginePhase = "stopped" | "starting" | "running" | "degraded" | "stopping" | "failed";
export type EngineMode = "proxy" | "tun";
export type AdapterHealth = "idle" | "healthy" | "unstable" | "cooldown" | "probing" | "failed";
export const HOME_TELEMETRY_POLL_MS = 800;

export type HomeAdapter = AdapterView & {
  downloadBPS: number;
  uploadBPS: number;
  connections: number;
  bytesDown: number;
  bytesUp: number;
  health: AdapterHealth;
  latencyMS?: number;
  jitterMS?: number;
  lossRate?: number;
};

const emptySnapshot = (mode: EngineMode = "proxy"): EngineSnapshot => ({
  phase: "stopped",
  mode,
  weighted: false,
  core_connected: false,
  core_elevated: false,
  download_bps: 0,
  upload_bps: 0,
  connections: 0,
  session_bytes: 0,
  adapters: [],
  sampled_at: new Date().toISOString(),
});

const previewAdapter = (index: number): AdapterView => {
  const sharedGatewayFixture =
    new URLSearchParams(window.location.search).get("fixture") === "visual-qa" && index < 2;
  const subnet = sharedGatewayFixture ? 10 : index + 1;
  return {
    id: index === 0 ? "Ethernet" : index === 1 ? "WLAN" : `Adapter ${index + 1}`,
    name: index === 0 ? "以太网" : index === 1 ? "WLAN" : `虚拟链路 ${index + 1}`,
    description: index === 0 ? "Realtek PCIe 2.5GbE" : index === 1 ? "Intel Wi-Fi 6E AX211" : "浏览器容量验证数据",
    address: `192.168.${subnet}.${100 + index}`,
    prefix_length: 24,
    if_index: 10 + index,
    dns_servers: [],
    gateway: `192.168.${subnet}.1`,
    metric: 25 + index,
    automatic_metric: true,
    selected: index < 2,
    weight: index === 0 ? 3 : index === 1 ? 2 : 1,
    kind: index % 2 === 0 ? "ethernet" : "wifi",
    operational: true,
  };
};

function browserFixtureCount() {
  const value = Number(new URLSearchParams(window.location.search).get("adapters") ?? 2);
  return [0, 1, 2, 4, 8, 16, 32].includes(value) ? value : 2;
}

function browserFixturePhase(): EnginePhase {
  const value = new URLSearchParams(window.location.search).get("engine");
  return value === "starting" || value === "running" || value === "degraded" || value === "stopping" || value === "failed"
    ? value
    : "stopped";
}

const isBrowserPreview = () => {
  const explicitVisualQA = new URLSearchParams(window.location.search).get("fixture") === "visual-qa";
  return !isDesktopRuntime() && (import.meta.env.DEV || explicitVisualQA);
};

const previewTunPreflight = (selected: AdapterView[]): TunPreflightSnapshot => {
  const foreign = new URLSearchParams(window.location.search).get("tun_preflight") === "foreign";
  const issues = [
    ...(foreign ? [{
      code: "foreign_tun",
      level: "blocker",
      title: "第三方虚拟隧道正在接管默认路由",
      detail: "检测到 Clash。请先关闭对应代理或 VPN，再启动虚拟网卡模式。",
    }] : []),
    ...(selected.length > 1 ? [{
      code: "shared_lan_gateway",
      level: "warning",
      title: "所选网卡共用子网和默认网关",
      detail: "以太网与 WLAN 同属 192.168.10.0/24，且共用网关 192.168.10.1；允许继续，但无法保证独立出口。",
    }] : []),
  ];
  return {
    ready: !foreign,
    checked_at: new Date().toISOString(),
    selected_adapter_ids: selected.map((adapter) => adapter.id),
    host_elevated: false,
    privilege_broker_available: true,
    engine_available: true,
    sing_box_available: true,
    wfp_ready: true,
    wfp_detail: "FwpmEngineOpen0 succeeded",
    strict_route_requested: true,
    effective_strict_route: true,
    shared_gateway_risks: selected.length > 1 ? ["以太网与 WLAN 共用网关 192.168.10.1"] : [],
    network_risks: [],
    issues,
  };
};

export const phaseText: Record<EnginePhase, string> = {
  stopped: "核心待命",
  starting: "正在建立聚合通道",
  running: "聚合引擎运行中",
  degraded: "聚合引擎降级运行",
  stopping: "正在安全停止",
  failed: "聚合核心异常",
};

const healthValue = (value: string | undefined): AdapterHealth => {
  if (
    value === "healthy" || value === "unstable" || value === "cooldown" ||
    value === "probing" || value === "failed"
  ) {
    return value;
  }
  return "idle";
};

export function useEngineState(
  onError: (message: string, retry?: () => void) => void,
  onTunPreflight?: (snapshot: TunPreflightSnapshot) => boolean | Promise<boolean>,
) {
  const [mode, setModeState] = useState<EngineMode>("proxy");
  const [weighted, setWeightedState] = useState(false);
  const [phase, setPhase] = useState<EnginePhase>("stopped");
  const [snapshot, setSnapshot] = useState<EngineSnapshot>(emptySnapshot());
  const [adapters, setAdapters] = useState<AdapterView[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [preview, setPreview] = useState(false);
  const [ports, setPorts] = useState({ socks: 10800, http: 10801 });
  const [history, setHistory] = useState<number[]>(Array.from({ length: 18 }, () => 0));
  const [diagnostics, setDiagnostics] = useState<DiagnosticResult[]>([]);
  const mounted = useRef(true);
  const modeRef = useRef<EngineMode>(mode);
  const weightedRef = useRef(weighted);
  const adaptersRef = useRef<AdapterView[]>(adapters);
  const lastRuntimeFailure = useRef("");
  const operationActive = useRef(false);
  const snapshotEpoch = useRef(0);
  const transition = phase === "starting" || phase === "stopping";
  const transitionRef = useRef(transition);
  const previewRef = useRef(preview);
  modeRef.current = mode;
  weightedRef.current = weighted;
  adaptersRef.current = adapters;
  transitionRef.current = transition;
  previewRef.current = preview;

  const applySnapshot = useCallback((
    next: EngineSnapshot,
    acceptDuringOperation = false,
    requestEpoch = snapshotEpoch.current,
  ) => {
    if (
      !mounted.current ||
      requestEpoch !== snapshotEpoch.current ||
      (operationActive.current && !acceptDuringOperation)
    ) return;
    const nextPhase = (["stopped", "starting", "running", "degraded", "stopping", "failed"].includes(next.phase)
      ? next.phase
      : "failed") as EnginePhase;
    setSnapshot(next);
    setPhase(nextPhase);
    if (next.mode === "proxy" || next.mode === "tun") {
      setModeState(next.mode);
    }
    void desktopPlatform.setEngineTrayStatus(nextPhase, next.mode);
    setWeightedState(next.weighted);
    setHistory((current) => [...current.slice(-17), next.download_bps / (1024 * 1024)]);
    const compatibilityNotice = next.reason?.startsWith("提示：") === true;
    if ((nextPhase === "failed" || nextPhase === "degraded" || compatibilityNotice) && next.reason && next.reason !== lastRuntimeFailure.current) {
      lastRuntimeFailure.current = next.reason;
      onError(next.reason);
    } else if (nextPhase === "running" && !compatibilityNotice) {
      lastRuntimeFailure.current = "";
    }
  }, [onError]);

  const load = useCallback(async (showError = true) => {
    if (isBrowserPreview()) {
      const fixtures = Array.from({ length: browserFixtureCount() }, (_, index) => previewAdapter(index));
      if (!mounted.current) return;
      adaptersRef.current = fixtures;
      setAdapters(fixtures);
      setPreview(true);
      applySnapshot({ ...emptySnapshot(modeRef.current), phase: browserFixturePhase() });
      setLoading(false);
      return;
    }

    // Adapter/settings data is enough to paint the usable home page. Core
    // negotiation and diagnostics continue independently so a cold Core
    // process cannot hold the entire adapter list behind one spinner.
    const requestEpoch = snapshotEpoch.current;
    const runtimeTask = Promise.all([
      appServices.engine.snapshot(),
      appServices.diagnostics.latest(),
    ]).then(([nextSnapshot, latestDiagnostics]) => {
      if (!mounted.current || requestEpoch !== snapshotEpoch.current) return;
      setDiagnostics(latestDiagnostics.results ?? []);
      applySnapshot(nextSnapshot, false, requestEpoch);
    }).catch((error) => {
      if (showError) {
        onError(error instanceof Error ? error.message : String(error), () => void load());
      }
    });

    try {
      const [nextAdapters, settings] = await Promise.all([
        appServices.adapters.list(),
        appServices.settings.get(),
      ]);
      if (!mounted.current) return;
      setAdapters(nextAdapters ?? []);
      adaptersRef.current = nextAdapters ?? [];
      setWeightedState(settings.weighted);
      weightedRef.current = settings.weighted;
      setPorts({ socks: settings.socks_port, http: settings.http_port });
      setPreview(false);
    } catch (error) {
      if (showError) {
        onError(error instanceof Error ? error.message : String(error), () => void load());
      }
    } finally {
      if (mounted.current) setLoading(false);
    }
    void runtimeTask;
  }, [applySnapshot, onError]);

  useEffect(() => {
    mounted.current = true;
    void load();
    const stopSnapshotPoll = startSerialPoll(async () => {
      if (!transitionRef.current && !previewRef.current && !adapterSaveQueue.isPending()) {
        const requestEpoch = snapshotEpoch.current;
        const next = await appServices.engine.snapshot();
        applySnapshot(next, false, requestEpoch);
      }
    }, HOME_TELEMETRY_POLL_MS);
    const stopAdapterPoll = startSerialPoll(async () => {
      if (transitionRef.current || previewRef.current || adapterSaveQueue.isPending()) return;
      const next = await appServices.adapters.list();
      if (mounted.current) {
        setAdapters((current) => {
          const currentKey = adapterListKey(current);
          const nextKey = adapterListKey(next ?? []);
          if (currentKey === nextKey) return current;
          adaptersRef.current = next ?? [];
          return adaptersRef.current;
        });
      }
    }, 5000);
    return () => {
      mounted.current = false;
      stopSnapshotPoll();
      stopAdapterPoll();
    };
  }, [applySnapshot, load]);

  const persistAdapters = useCallback((next: AdapterView[], nextMode = modeRef.current, nextWeighted = weightedRef.current) => {
    adaptersRef.current = next;
    setAdapters(next);
    if (preview) return Promise.resolve(next);
    const handle = adapterSaveQueue.enqueue(adapterSaveInput(nextMode, nextWeighted, next));
    void handle.done.then((saved) => {
      if (!mounted.current || !adapterSaveQueue.isCurrent(handle.revision)) return;
      const authoritative = saved ?? next;
      adaptersRef.current = authoritative;
      setAdapters(authoritative);
    }).catch((error) => {
      if (!adapterSaveQueue.isCurrent(handle.revision)) return;
      onError(error instanceof Error ? error.message : String(error), () => void persistAdapters(next, nextMode, nextWeighted));
      void load(false);
    });
    return handle.done;
  }, [load, onError, preview]);

  const setMode = useCallback((nextMode: EngineMode) => {
    setModeState(nextMode);
    modeRef.current = nextMode;
    void persistAdapters(adaptersRef.current, nextMode, weightedRef.current);
  }, [persistAdapters]);

  const setWeighted = useCallback((value: boolean) => {
    setWeightedState(value);
    weightedRef.current = value;
    void persistAdapters(adaptersRef.current, modeRef.current, value);
  }, [persistAdapters]);

  const toggleAdapter = useCallback((id: string, checked: boolean) => {
    void persistAdapters(adaptersRef.current.map((adapter) => adapter.id === id ? { ...adapter, selected: checked } : adapter));
  }, [persistAdapters]);

  const updateWeight = useCallback((id: string, value: number) => {
    if (!Number.isInteger(value) || value < 1 || value > 100) return;
    void persistAdapters(adaptersRef.current.map((adapter) => adapter.id === id ? { ...adapter, weight: value } : adapter));
  }, [persistAdapters]);

  const selectAll = useCallback((checked: boolean) => {
    void persistAdapters(adaptersRef.current.map((adapter) => ({ ...adapter, selected: checked })));
  }, [persistAdapters]);

  const refreshAdapters = useCallback(async () => {
    setRefreshing(true);
    try {
      if (preview) {
        const refreshed = Array.from({ length: browserFixtureCount() }, (_, index) => previewAdapter(index));
        adaptersRef.current = refreshed;
        setAdapters(refreshed);
      } else {
        const refreshed = await appServices.adapters.refresh();
        if (mounted.current) {
          adaptersRef.current = refreshed ?? [];
          setAdapters(refreshed ?? []);
        }
      }
    } catch (error) {
      onError(error instanceof Error ? error.message : String(error), () => void refreshAdapters());
    } finally {
      if (mounted.current) setRefreshing(false);
    }
  }, [onError, preview]);

  const toggleEngine = useCallback(async () => {
    if (transition || operationActive.current) return;
    operationActive.current = true;
    const operationEpoch = ++snapshotEpoch.current;
    const stopping = phase === "running" || phase === "degraded";
    let operationFailed = false;
    setPhase(stopping ? "stopping" : "starting");
    try {
      if (!stopping && !preview) {
        await adapterSaveQueue.flush();
      }
      if (!stopping && mode === "tun") {
        let preflight: TunPreflightSnapshot;
        const selectedAdapters = adapters.filter((adapter) => adapter.selected);
        if (isBrowserPreview()) {
          preflight = previewTunPreflight(selectedAdapters);
        } else {
          preflight = await appServices.tun.preflight(selectedAdapters.map((adapter) => adapter.id));
        }
        const hasRisks = (preflight.issues ?? []).some((issue) => issue.level !== "info");
        const confirmed = hasRisks && onTunPreflight
          ? await onTunPreflight(preflight)
          : preflight.ready;
        if (!preflight.ready || !confirmed) {
          setPhase("stopped");
          return;
        }
        if (
          isBrowserPreview() &&
          new URLSearchParams(window.location.search).get("uac") === "cancel"
        ) {
          throw new Error("用户取消了管理员权限请求");
        }
      }
      if (!stopping && mode === "proxy") {
        // The Steam notice is advisory and must never sit on the engine's
        // critical startup path. Process discovery continues independently.
        void appServices.routing.listProcesses().then((processes) => {
          if (mounted.current && (processes ?? []).some((name) => name.toLowerCase() === "steam.exe")) {
            onError("检测到 Steam 正在运行，请重启 Steam 客户端以使多链路加速完全生效。");
          }
        }).catch(() => undefined);
      }
      const next = await withServiceTimeout(
        stopping ? appServices.engine.stop() : appServices.engine.start(mode),
        stopping ? 40_000 : 55_000,
        stopping ? "Stopping aggregation" : "Starting aggregation",
      );
      applySnapshot(next, true, operationEpoch);
      await load(false);
    } catch (error) {
      operationFailed = true;
      setPhase(stopping ? "running" : "stopped");
      const message = error instanceof Error ? error.message : String(error);
      const elevationCancelled =
        message.includes("用户取消了管理员权限请求") ||
        message.includes("ERROR_CANCELLED") ||
        message.includes("operation was canceled by the user");
      if (!stopping && mode === "tun" && elevationCancelled) {
        onError("未获得管理员权限，聚合未启动");
      } else {
        onError(message, () => void toggleEngine());
      }
    } finally {
      operationActive.current = false;
      if (operationFailed && mounted.current) {
        const recoveryEpoch = ++snapshotEpoch.current;
        try {
          const recovered = await withServiceTimeout(
            appServices.engine.snapshot(),
            8_000,
            "Refreshing aggregation state",
          );
          applySnapshot(recovered, true, recoveryEpoch);
        } catch {
          if (mounted.current) setPhase(stopping ? "running" : "stopped");
        }
      }
    }
  }, [adapters, applySnapshot, load, mode, onError, onTunPreflight, phase, preview, transition]);

  const selected = useMemo(() => adapters.filter((adapter) => adapter.selected), [adapters]);
  const runtimeByID = useMemo(
    () => new Map((snapshot.adapters ?? []).map((adapter) => [adapter.id, adapter])),
    [snapshot.adapters],
  );
  const diagnosticByID = useMemo(
    () => new Map(diagnostics.map((result) => [result.adapter_id, result])),
    [diagnostics],
  );
  const homeAdapters: HomeAdapter[] = useMemo(
    () => adapters.map((adapter) => {
      const runtime = runtimeByID.get(adapter.name) ?? runtimeByID.get(adapter.id);
      const diagnostic = diagnosticByID.get(adapter.id);
      const diagnosticHealth = diagnostic?.status === "available"
        ? "healthy"
        : diagnostic?.status === "unstable"
          ? "unstable"
          : diagnostic?.status === "unavailable"
            ? "failed"
            : undefined;
      return {
        ...adapter,
        downloadBPS: runtime?.download_bps ?? 0,
        uploadBPS: runtime?.upload_bps ?? 0,
        connections: runtime?.connections ?? 0,
        bytesDown: runtime?.bytes_down ?? 0,
        bytesUp: runtime?.bytes_up ?? 0,
        health: diagnosticHealth ?? healthValue(runtime?.health_state),
        latencyMS: diagnostic?.avg_latency_ms,
        jitterMS: diagnostic?.jitter_ms,
        lossRate: diagnostic?.loss_rate,
      };
    }),
    [adapters, diagnosticByID, runtimeByID],
  );
  const totalWeight = selected.reduce((sum, adapter) => sum + adapter.weight, 0);

  return {
    phase, mode, weighted, adapters: homeAdapters, selected, totalWeight, history,
    loading, refreshing, preview, transitioning: transition,
    coreConnected: snapshot.core_connected, coreVersion: snapshot.core_version ?? "—",
    coreElevated: snapshot.core_elevated,
    ports,
    totalDownload: snapshot.download_bps,
    totalUpload: snapshot.upload_bps,
    totalConnections: snapshot.connections,
    sessionBytes: snapshot.session_bytes,
    setMode, setWeighted, toggleEngine, toggleAdapter, updateWeight, selectAll, refreshAdapters,
  };
}
