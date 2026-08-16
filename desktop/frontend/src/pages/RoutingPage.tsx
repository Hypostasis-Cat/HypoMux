import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Button,
  createTableColumn,
  DataGrid,
  DataGridBody,
  DataGridCell,
  DataGridHeader,
  DataGridHeaderCell,
  DataGridRow,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  Dropdown,
  Input,
  MessageBar,
  MessageBarBody,
  Option,
  SearchBox,
  Spinner,
  Tab,
  TabList,
  Toast,
  ToastBody,
  ToastTitle,
  Toolbar,
  ToolbarButton,
  useId,
  useToastController,
  type TableColumnDefinition,
  type TableRowId,
} from "@fluentui/react-components";
import { AppToaster } from "../components/AppToaster";
import {
  Add20Regular,
  ArrowDownload20Regular,
  ArrowUpload20Regular,
  AppsList20Regular,
  CheckmarkCircle16Regular,
  Delete20Regular,
  ErrorCircle16Regular,
  Save20Regular,
} from "@fluentui/react-icons";
import { appServices, type RoutingRule, type RoutingSnapshot } from "../platform/services";
import { GlassSurface } from "../components/material/GlassSurface";
import { useI18n } from "../i18n/i18n";
import { isDesktopRuntime } from "../platform/runtime";
import { LatestSaveQueue } from "../platform/latestSaveQueue";

type MatchType = "process" | "domain" | "ip";
type DraftRule = RoutingRule & {
  id: string;
  error?: string;
  validating?: boolean;
  dirty?: boolean;
};

const newID = () => globalThis.crypto?.randomUUID?.() ?? `rule-${Date.now()}-${Math.random()}`;

export const makeDrafts = (rules: RoutingRule[]): DraftRule[] =>
  rules.map((rule) => ({ ...rule, id: newID() }));

const ruleKey = (rule: RoutingRule) => `${rule.match_type}\u0000${rule.value}\u0000${rule.outbound}`;

export const reconcileSavedDrafts = (saved: RoutingRule[], submitted: DraftRule[]): DraftRule[] => {
  const available = new Map<string, DraftRule[]>();
  submitted.forEach((draft) => {
    const key = ruleKey(draft);
    available.set(key, [...(available.get(key) ?? []), draft]);
  });
  return saved.map((rule) => {
    const matches = available.get(ruleKey(rule)) ?? [];
    const existing = matches.shift();
    if (matches.length > 0) available.set(ruleKey(rule), matches);
    else available.delete(ruleKey(rule));
    return { ...rule, id: existing?.id ?? newID() };
  });
};

const browserRoutingFixture = (): RoutingSnapshot | null => {
  if (!import.meta.env.DEV || isDesktopRuntime()) return null;
  const count = Math.max(0, Math.min(500, Number(new URLSearchParams(window.location.search).get("rules") ?? 60)));
  const types: MatchType[] = ["process", "domain", "ip"];
  return {
    outbounds: [
      { id: "aggregation", label: "多网卡聚合" },
      { id: "direct", label: "直连 / 绕过" },
      { id: "nic_以太网", label: "以太网" },
      { id: "nic_WLAN", label: "WLAN" },
    ],
    rules: Array.from({ length: count }, (_, index) => {
      const type = types[index % types.length];
      return {
        match_type: type,
        value: type === "process"
          ? `application-${String(index + 1).padStart(2, "0")}.exe`
          : type === "domain"
            ? `service-${index + 1}.example.com`
            : `10.${Math.floor(index / 256)}.${index % 256}.0/24`,
        outbound: index % 4 === 0 ? "direct" : index % 4 === 1 ? "nic_以太网" : "aggregation",
      };
    }),
  };
};

export function RoutingPage() {
  const { locale, t } = useI18n();
  const text = useCallback((zh: string, en: string) => locale === "en" ? en : zh, [locale]);
  const matchLabels = useMemo<Record<MatchType, string>>(() => ({
    process: t("routing_tab_process"),
    domain: t("routing_tab_domain"),
    ip: t("routing_tab_ip"),
  }), [t]);
  const placeholders = useMemo<Record<MatchType, string>>(() => ({
    process: t("routing_placeholder_process"),
    domain: t("routing_placeholder_domain"),
    ip: t("routing_placeholder_ip"),
  }), [t]);
  const [rules, setRules] = useState<DraftRule[]>([]);
  const [outbounds, setOutbounds] = useState<RoutingSnapshot["outbounds"]>([]);
  const [activeType, setActiveType] = useState<MatchType>("process");
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<Set<TableRowId>>(new Set());
  const [newValue, setNewValue] = useState("");
  const [newOutbound, setNewOutbound] = useState("aggregation");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState("");
  const [pendingSave, setPendingSave] = useState(false);
  const [engineRunningInTun, setEngineRunningInTun] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [processOpen, setProcessOpen] = useState(false);
  const [processes, setProcesses] = useState<string[]>([]);
  const [processSearch, setProcessSearch] = useState("");
  const [processLoading, setProcessLoading] = useState(false);
  const [importPreview, setImportPreview] = useState<RoutingSnapshot | null>(null);
  const [importPreviewOpen, setImportPreviewOpen] = useState(false);
  const loaded = useRef(false);
  const rulesRef = useRef<DraftRule[]>([]);
  const editRevision = useRef(0);
  const autosaveTimer = useRef<number>();
  const validationSequence = useRef(new Map<string, number>());
  const validationTimers = useRef(new Map<string, number>());
  const saveQueue = useRef<LatestSaveQueue<RoutingRule[], RoutingSnapshot>>();
  if (!saveQueue.current) {
    saveQueue.current = new LatestSaveQueue((next) => appServices.routing.save(next));
  }
  const addRuleInputRef = useRef<HTMLInputElement>(null);
  const toasterId = useId("routing-toaster");
  const { dispatchToast } = useToastController(toasterId);

  const notify = useCallback((title: string, message: string, intent: "success" | "error" | "info" = "info") => {
    dispatchToast(
      <Toast>
        <ToastTitle>{title}</ToastTitle>
        <ToastBody>{message}</ToastBody>
      </Toast>,
      { intent, timeout: intent === "error" ? 6000 : 2600 },
    );
  }, [dispatchToast]);

  const load = useCallback(async () => {
    setLoading(true);
    const engineTask = appServices.engine.snapshot()
      .then((engine) => {
        setEngineRunningInTun(engine.phase === "running" && engine.mode === "tun");
      })
      .catch(() => undefined);
    try {
      const snapshot = await appServices.routing.snapshot();
      const available = new Set((snapshot.outbounds ?? []).map((outbound) => outbound.id));
      const nextRules = makeDrafts(snapshot.rules ?? []).map((rule) => available.has(rule.outbound)
        ? rule
        : {
            ...rule,
            error: text(
              `出口 ${rule.outbound.replace(/^nic_/, "")} 未启用或当前不可用`,
              `Outbound ${rule.outbound.replace(/^nic_/, "")} is disabled or unavailable`,
            ),
          });
      rulesRef.current = nextRules;
      setRules(nextRules);
      setOutbounds(snapshot.outbounds ?? []);
      setPendingSave(false);
      setSavedAt(new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));
      loaded.current = true;
    } catch (error) {
      const fixture = browserRoutingFixture();
      if (fixture) {
        const nextRules = makeDrafts(fixture.rules ?? []);
        rulesRef.current = nextRules;
        setRules(nextRules);
        setOutbounds(fixture.outbounds ?? []);
        setSavedAt(text("浏览器容量预览", "Browser capacity preview"));
        loaded.current = true;
      } else {
        notify(text("无法读取分流规则", "Unable to load routing rules"), error instanceof Error ? error.message : String(error), "error");
      }
    } finally {
      setLoading(false);
    }
    void engineTask;
  }, [notify, text]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => () => {
    if (autosaveTimer.current !== undefined) window.clearTimeout(autosaveTimer.current);
    validationTimers.current.forEach((timer) => window.clearTimeout(timer));
  }, []);

  const applyRules = useCallback((next: DraftRule[], markDirty = false) => {
    rulesRef.current = next;
    setRules(next);
    if (markDirty) {
      editRevision.current += 1;
      setPendingSave(true);
    }
  }, []);

  const validateDraft = useCallback(async (draft: DraftRule) => {
    const sequence = (validationSequence.current.get(draft.id) ?? 0) + 1;
    validationSequence.current.set(draft.id, sequence);
    try {
      const currentRules = rulesRef.current
        .filter((item) => item.id !== draft.id)
        .map(({ match_type, value, outbound }) => ({ match_type, value, outbound }));
      const result = await appServices.routing.validate(draft, currentRules);
      if (validationSequence.current.get(draft.id) !== sequence) return;
      const next = rulesRef.current.map((item) =>
        item.id === draft.id
          ? { ...item, ...result.rule, validating: false, error: result.valid ? undefined : result.message, dirty: true }
          : item);
      applyRules(next);
    } catch (error) {
      if (validationSequence.current.get(draft.id) !== sequence) return;
      const next = rulesRef.current.map((item) =>
        item.id === draft.id
          ? { ...item, validating: false, error: error instanceof Error ? error.message : String(error), dirty: true }
          : item);
      applyRules(next);
    }
  }, [applyRules]);

  const updateRule = useCallback((id: string, patch: Partial<RoutingRule>) => {
    let nextDraft: DraftRule | undefined;
    const next = rulesRef.current.map((item) => {
      if (item.id !== id) return item;
      nextDraft = { ...item, ...patch, validating: true, error: undefined, dirty: true };
      return nextDraft;
    });
    applyRules(next, true);
    const existingTimer = validationTimers.current.get(id);
    if (existingTimer !== undefined) window.clearTimeout(existingTimer);
    const timer = window.setTimeout(() => {
      validationTimers.current.delete(id);
      if (nextDraft) void validateDraft(nextDraft);
    }, 180);
    validationTimers.current.set(id, timer);
  }, [applyRules, validateDraft]);

  const saveRules = useCallback(async (showToast = false) => {
    if (showToast && autosaveTimer.current !== undefined) {
      window.clearTimeout(autosaveTimer.current);
      autosaveTimer.current = undefined;
    }
    const submitted = rulesRef.current.map((rule) => ({ ...rule }));
    const submittedEditRevision = editRevision.current;
    const invalid = submitted.find((rule) => rule.validating || rule.error || !rule.value.trim());
    if (invalid) {
      if (showToast) notify(
        text("尚未保存", "Not saved"),
        invalid.error || text("请等待规则校验完成", "Please wait for rule validation to finish"),
        "error",
      );
      return false;
    }
    setSaving(true);
    const queue = saveQueue.current!;
    const handle = queue.enqueue(
      submitted.map(({ match_type, value, outbound }) => ({ match_type, value, outbound })),
    );
    try {
      const snapshot = await handle.done;
      if (!queue.isCurrent(handle.revision) || submittedEditRevision !== editRevision.current) return true;
      const savedRules = reconcileSavedDrafts(snapshot.rules ?? [], submitted);
      applyRules(savedRules);
      setOutbounds(snapshot.outbounds ?? []);
      setSavedAt(new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }));
      setPendingSave(false);
      if (showToast) notify(
        text("规则已保存", "Rules saved"),
        text(
          `已持久化 ${snapshot.rules?.length ?? 0} 条分流规则`,
          `${snapshot.rules?.length ?? 0} routing rules were persisted`,
        ),
        "success",
      );
      return true;
    } catch (error) {
      if (queue.isCurrent(handle.revision)) {
        notify(text("保存失败", "Save failed"), error instanceof Error ? error.message : String(error), "error");
      }
      return false;
    } finally {
      if (queue.isCurrent(handle.revision)) setSaving(false);
    }
  }, [applyRules, notify, text]);

  useEffect(() => {
    if (!loaded.current || !pendingSave || rules.some((rule) => rule.validating || rule.error)) {
      return;
    }
    autosaveTimer.current = window.setTimeout(() => {
      autosaveTimer.current = undefined;
      void saveRules(false);
    }, 700);
    return () => {
      if (autosaveTimer.current !== undefined) {
        window.clearTimeout(autosaveTimer.current);
        autosaveTimer.current = undefined;
      }
    };
  }, [pendingSave, rules, saveRules]);

  const addRule = useCallback(async (value = newValue, type: MatchType = activeType) => {
    const candidate: DraftRule = {
      id: newID(),
      match_type: type,
      value,
      outbound: newOutbound,
      validating: true,
      dirty: true,
    };
    const result = await appServices.routing.validate(
      candidate,
      rulesRef.current.map(({ match_type, value: itemValue, outbound }) => ({ match_type, value: itemValue, outbound })),
    );
    if (!result.valid) {
      notify(
        result.duplicate ? text("规则已存在", "Rule already exists") : text("规则格式无效", "Invalid rule format"),
        result.message || text("请检查匹配值", "Check the match value"),
        "error",
      );
      return;
    }
    applyRules([...rulesRef.current, { ...candidate, ...result.rule, validating: false }], true);
    setNewValue("");
  }, [activeType, applyRules, newOutbound, newValue, notify, text]);

  const openProcesses = useCallback(async () => {
    setProcessOpen(true);
    setProcessLoading(true);
    setProcessSearch("");
    try {
      setProcesses((await appServices.routing.listProcesses()) ?? []);
    } catch (error) {
      notify(text("无法读取进程", "Unable to list processes"), error instanceof Error ? error.message : String(error), "error");
      setProcesses([]);
    } finally {
      setProcessLoading(false);
    }
  }, [notify, text]);

  const importRules = useCallback(async () => {
    try {
      const preview = await appServices.routing.importRules();
      setImportPreview(preview);
      setImportPreviewOpen(true);
    } catch (error) {
      notify(text("导入失败", "Import failed"), error instanceof Error ? error.message : String(error), "error");
    }
  }, [notify, text]);

  const confirmImport = useCallback(async () => {
    if (!importPreview) return;
    if (autosaveTimer.current !== undefined) {
      window.clearTimeout(autosaveTimer.current);
      autosaveTimer.current = undefined;
    }
    const previous = rulesRef.current;
    const imported = makeDrafts(importPreview.rules ?? []);
    applyRules(imported, true);
    setPendingSave(false);
    setSaving(true);
    const queue = saveQueue.current!;
    const importedEditRevision = editRevision.current;
    const handle = queue.enqueue(importPreview.rules ?? []);
    try {
      const saved = await handle.done;
      if (!queue.isCurrent(handle.revision) || importedEditRevision !== editRevision.current) return;
      applyRules(reconcileSavedDrafts(saved.rules ?? [], imported));
      setOutbounds(saved.outbounds ?? []);
      setImportPreviewOpen(false);
      setSelected(new Set());
      setPendingSave(false);
      notify(
        text("导入完成", "Import complete"),
        text(
          `已原子替换为 ${saved.rules?.length ?? 0} 条规则`,
          `Atomically replaced the current list with ${saved.rules?.length ?? 0} rules`,
        ),
        "success",
      );
    } catch (error) {
      if (queue.isCurrent(handle.revision) && importedEditRevision === editRevision.current) {
        applyRules(previous, true);
        notify(text("导入失败", "Import failed"), error instanceof Error ? error.message : String(error), "error");
      }
    } finally {
      if (queue.isCurrent(handle.revision)) setSaving(false);
    }
  }, [applyRules, importPreview, notify, text]);

  const activeRules = useMemo(() => rules.filter((rule) => {
    if (rule.match_type !== activeType) return false;
    const keyword = filter.trim().toLowerCase();
    return !keyword || rule.value.toLowerCase().includes(keyword) ||
      (outbounds ?? []).find((outbound) => outbound.id === rule.outbound)?.label.toLowerCase().includes(keyword);
  }), [activeType, filter, outbounds, rules]);

  const outboundLabel = useCallback((id: string) => {
    if (id === "aggregation") return t("routing_outbound_aggregation");
    if (id === "direct") return t("routing_outbound_direct");
    return (outbounds ?? []).find((outbound) => outbound.id === id)?.label ?? id.replace(/^nic_/, "");
  }, [outbounds, t]);

  const columns: TableColumnDefinition<DraftRule>[] = useMemo(() => [
    createTableColumn<DraftRule>({
      columnId: "value",
      renderHeaderCell: () => text("匹配值", "Match value"),
      renderCell: (item) => (
        <Input
          className="routing-cell-input"
          value={item.value}
          appearance="filled-darker"
          aria-invalid={Boolean(item.error)}
          onChange={(_, data) => updateRule(item.id, { value: data.value })}
        />
      ),
    }),
    createTableColumn<DraftRule>({
      columnId: "outbound",
      renderHeaderCell: () => t("routing_col_nic"),
      renderCell: (item) => (
        <Dropdown
          className="routing-cell-dropdown"
          appearance="filled-darker"
          value={outboundLabel(item.outbound)}
          selectedOptions={[item.outbound]}
          onOptionSelect={(_, data) => data.optionValue && updateRule(item.id, { outbound: data.optionValue })}
        >
          {(outbounds ?? []).map((outbound) => (
            <Option key={outbound.id} value={outbound.id}>{outboundLabel(outbound.id)}</Option>
          ))}
          {!(outbounds ?? []).some((outbound) => outbound.id === item.outbound) && (
            <Option value={item.outbound}>{outboundLabel(item.outbound)}</Option>
          )}
        </Dropdown>
      ),
    }),
    createTableColumn<DraftRule>({
      columnId: "status",
      renderHeaderCell: () => text("状态", "Status"),
      renderCell: (item) => item.validating
        ? <span className="routing-rule-status is-validating"><Spinner size="tiny" />{text("校验中", "Validating")}</span>
        : item.error
          ? <span className="routing-rule-status is-error" title={item.error}><ErrorCircle16Regular /><span>{item.error}</span></span>
          : <span className="routing-rule-status is-valid"><CheckmarkCircle16Regular />{text("有效", "Valid")}</span>,
    }),
  ], [outboundLabel, outbounds, t, text, updateRule]);

  const counts = useMemo(() => ({
    process: rules.filter((rule) => rule.match_type === "process").length,
    domain: rules.filter((rule) => rule.match_type === "domain").length,
    ip: rules.filter((rule) => rule.match_type === "ip").length,
  }), [rules]);

  const filteredProcesses = useMemo(() => {
    const keyword = processSearch.trim().toLowerCase();
    return processes.filter((process) => !keyword || process.toLowerCase().includes(keyword));
  }, [processSearch, processes]);

  return (
    <main className="routing-page">
      <AppToaster toasterId={toasterId} position="top-end" />
      <header className="routing-page-heading">
        <div>
          <span className="section-kicker">{t("routing_title")}</span>
          <h1>{text("决定每类流量从哪条链路离开", "Choose the exit path for each type of traffic")}</h1>
          <p>{t("routing_hint")}</p>
        </div>
        <div className="routing-save-state">
          <span key={saving ? "saving" : pendingSave ? "pending" : savedAt ? "saved" : "loading"} className="motion-inline-swap">{saving
            ? text("正在保存…", "Saving…")
            : pendingSave
              ? text("有未保存更改", "Unsaved changes")
            : savedAt
              ? text(`已保存 ${savedAt}`, `Saved ${savedAt}`)
              : text("读取当前配置", "Loading current configuration")}</span>
          <Button appearance={pendingSave ? "primary" : "secondary"} icon={saving ? <Spinner size="tiny" /> : <Save20Regular />} disabled={saving || !pendingSave} onClick={() => void saveRules(true)}>
            {text("保存更改", "Save changes")}
          </Button>
        </div>
      </header>

      <div className="routing-notice-slot">
        {engineRunningInTun && (
          <MessageBar intent="warning">
            <MessageBarBody>{text(
              "规则会立即保存，但当前 sing-box 不热重载路由；停止并重新启动聚合后生效。",
              "Rules are saved immediately, but sing-box does not hot-reload routing. Stop and restart aggregation to apply them.",
            )}</MessageBarBody>
          </MessageBar>
        )}
      </div>

      <GlassSurface className="routing-toolbar-surface" tone="secondary">
        <TabList selectedValue={activeType} onTabSelect={(_, data) => {
          setActiveType(data.value as MatchType);
          setSelected(new Set());
        }}>
          {(Object.keys(matchLabels) as MatchType[]).map((type) => (
            <Tab key={type} value={type}>{matchLabels[type]} · {counts[type]}</Tab>
          ))}
        </TabList>
        <div className="routing-add-row">
          <Input
            ref={addRuleInputRef}
            value={newValue}
            placeholder={placeholders[activeType]}
            onChange={(_, data) => setNewValue(data.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && newValue.trim()) void addRule();
            }}
          />
          <Dropdown
            value={outboundLabel(newOutbound)}
            selectedOptions={[newOutbound]}
            onOptionSelect={(_, data) => data.optionValue && setNewOutbound(data.optionValue)}
          >
            {(outbounds ?? []).map((outbound) => (
              <Option key={outbound.id} value={outbound.id}>{outboundLabel(outbound.id)}</Option>
            ))}
          </Dropdown>
          <Button appearance="primary" icon={<Add20Regular />} disabled={!newValue.trim()} onClick={() => void addRule()}>
            {t("routing_add")}
          </Button>
          {activeType === "process" && (
            <Button icon={<AppsList20Regular />} onClick={() => void openProcesses()}>{t("routing_select_process")}</Button>
          )}
        </div>
        <Toolbar className="routing-actions" aria-label={text("规则操作", "Rule actions")}>
          <SearchBox value={filter} placeholder={text("筛选当前类型", "Filter current type")} onChange={(_, data) => setFilter(data.value)} />
          <span>{text(`${activeRules.length} 条显示 · ${rules.length} 条总计`, `${activeRules.length} shown · ${rules.length} total`)}</span>
          <ToolbarButton icon={<Delete20Regular />} disabled={selected.size === 0} onClick={() => setDeleteOpen(true)}>
            {text(`删除选中 (${selected.size})`, `Delete selected (${selected.size})`)}
          </ToolbarButton>
          <ToolbarButton icon={<ArrowDownload20Regular />} onClick={() => void importRules()}>{t("routing_import")}</ToolbarButton>
          <ToolbarButton icon={<ArrowUpload20Regular />} onClick={() => void appServices.routing.exportRules(
            rules.map(({ match_type, value, outbound }) => ({ match_type, value, outbound })),
          ).then((path) => path && notify(text("导出完成", "Export complete"), path, "success")).catch((error) =>
            notify(text("导出失败", "Export failed"), error instanceof Error ? error.message : String(error), "error"))}>
            {text("导出 / 分享", "Export / Share")}
          </ToolbarButton>
        </Toolbar>
      </GlassSurface>

      <GlassSurface className={`routing-grid-surface${loading || activeRules.length === 0 ? " is-empty" : ""}`}>
        {loading ? (
          <div key="routing-loading" className="routing-empty motion-state-content"><Spinner label={text("正在读取真实规则配置", "Loading routing configuration")} /></div>
        ) : activeRules.length === 0 ? (
          <div key={`routing-empty-${activeType}-${filter ? "filtered" : "plain"}`} className="routing-empty motion-state-content">
            <strong>{filter
              ? text("没有匹配筛选条件的规则", "No rules match the filter")
              : text(`尚未添加${matchLabels[activeType]}`, `No ${matchLabels[activeType]} have been added`)}</strong>
            <span>{filter
              ? text("清除筛选后查看全部规则。", "Clear the filter to see every rule.")
              : text(`使用上方输入框添加第一条规则，${placeholders[activeType]}。`, `Use the field above to add the first rule. ${placeholders[activeType]}.`)}</span>
            {!filter && (
              <Button
                appearance="primary"
                icon={<Add20Regular />}
                onClick={() => addRuleInputRef.current?.focus()}
              >
                {text("添加第一条规则", "Add first rule")}
              </Button>
            )}
          </div>
        ) : (
          <DataGrid
            className="routing-grid"
            items={activeRules}
            columns={columns}
            getRowId={(item) => item.id}
            selectionMode="multiselect"
            selectedItems={selected}
            onSelectionChange={(_, data) => setSelected(data.selectedItems)}
            sortable={false}
            resizableColumns
            columnSizingOptions={{
              value: { minWidth: 280, defaultWidth: 520 },
              outbound: { minWidth: 210, defaultWidth: 280 },
              status: { minWidth: 140, defaultWidth: 170 },
            }}
          >
            <DataGridHeader>
              <DataGridRow className="routing-grid-header" selectionCell={{ checkboxIndicator: { "aria-label": text("全选当前规则", "Select all current rules") } }}>
                {({ renderHeaderCell }) => <DataGridHeaderCell>{renderHeaderCell()}</DataGridHeaderCell>}
              </DataGridRow>
            </DataGridHeader>
            <DataGridBody<DraftRule>>
              {({ item, rowId }) => (
                <DataGridRow<DraftRule>
                  key={rowId}
                  className={`routing-row${selected.has(rowId) ? " is-selected" : ""}${item.error ? " has-error" : ""}`}
                  selectionCell={{ checkboxIndicator: { "aria-label": text(`选择 ${item.value}`, `Select ${item.value}`) } }}
                >
                  {({ renderCell }) => <DataGridCell>{renderCell(item)}</DataGridCell>}
                </DataGridRow>
              )}
            </DataGridBody>
          </DataGrid>
        )}
      </GlassSurface>

      <Dialog open={deleteOpen} onOpenChange={(_, data) => setDeleteOpen(data.open)}>
        <DialogSurface>
          <DialogBody>
            <DialogTitle>{text("删除选中的规则？", "Delete selected rules?")}</DialogTitle>
            <DialogContent>{text(
              `将删除 ${selected.size} 条规则。保存后不能从当前列表恢复。`,
              `${selected.size} rules will be deleted and cannot be restored from this list after saving.`,
            )}</DialogContent>
            <DialogActions>
              <Button appearance="secondary" onClick={() => setDeleteOpen(false)}>{t("routing_dialog_cancel")}</Button>
              <Button appearance="primary" onClick={() => {
                selected.forEach((id) => {
                  const timer = validationTimers.current.get(String(id));
                  if (timer !== undefined) window.clearTimeout(timer);
                  validationTimers.current.delete(String(id));
                });
                applyRules(rulesRef.current.filter((rule) => !selected.has(rule.id)).map((rule) => ({ ...rule, dirty: true })), true);
                setSelected(new Set());
                setDeleteOpen(false);
              }}>{text("确认删除", "Delete")}</Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      <Dialog open={processOpen} onOpenChange={(_, data) => setProcessOpen(data.open)}>
        <DialogSurface className="process-dialog">
          <DialogBody>
            <DialogTitle>{t("routing_process_dialog_title")}</DialogTitle>
            <DialogContent>
              <SearchBox autoFocus value={processSearch} placeholder={t("routing_process_search_placeholder")} onChange={(_, data) => setProcessSearch(data.value)} />
              <div className="process-list">
                {processLoading ? <Spinner label={t("routing_process_loading")} /> : filteredProcesses.length === 0
                  ? <span>{t("routing_process_empty")}</span>
                  : filteredProcesses.map((process) => (
                    <button key={process} onDoubleClick={() => {
                      setActiveType("process");
                      setProcessOpen(false);
                      void addRule(process, "process");
                    }} onClick={() => setNewValue(process)} className={newValue === process ? "is-selected" : ""}>
                      {process}
                    </button>
                  ))}
              </div>
            </DialogContent>
            <DialogActions>
              <Button onClick={() => setProcessOpen(false)}>{t("routing_dialog_cancel")}</Button>
              <Button appearance="primary" disabled={!newValue} onClick={() => {
                setActiveType("process");
                setProcessOpen(false);
                void addRule(newValue, "process");
              }}>{text("添加进程规则", "Add process rule")}</Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>

      <Dialog open={importPreviewOpen} onOpenChange={(_, data) => !data.open && setImportPreviewOpen(false)}>
        <DialogSurface>
          <DialogBody>
            <DialogTitle>{text("替换当前分流规则？", "Replace current routing rules?")}</DialogTitle>
            <DialogContent>
              {text(
                `文件已通过格式、版本和全部规则校验。确认后将用 ${importPreview?.rules?.length ?? 0} 条规则原子替换当前 ${rules.length} 条规则。`,
                `The file passed format, version, and rule validation. Confirm to atomically replace the current ${rules.length} rules with ${importPreview?.rules?.length ?? 0} imported rules.`,
              )}
            </DialogContent>
            <DialogActions>
              <Button onClick={() => setImportPreviewOpen(false)}>{t("routing_dialog_cancel")}</Button>
              <Button appearance="primary" onClick={() => void confirmImport()}>{text("确认导入", "Import and replace")}</Button>
            </DialogActions>
          </DialogBody>
        </DialogSurface>
      </Dialog>
    </main>
  );
}
