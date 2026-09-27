import { Field, Dialog, DialogSurface, DialogBody, DialogTitle, DialogContent, DialogActions, SearchBox, Badge, Button, Dropdown, Input, MessageBar, MessageBarBody, Option, Spinner } from "@fluentui/react-components";
import { ArrowClockwise20Regular, Add20Regular, Delete16Regular, ChevronDown16Regular, ChevronRight16Regular } from "@fluentui/react-icons";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { appServices, type RuleSet, type RuleSetEntries } from "../platform/services";
import { useI18n } from "../i18n/i18n";
import { GlassSurface } from "./material/GlassSurface";

export type RuleSetOutboundOption = { id: string; label: string };

type DraftRuleSet = {
  name: string;
  url: string;
  outbound: string;
  priority: string;
};

const emptyDraft: DraftRuleSet = { name: "", url: "", outbound: "direct", priority: "0" };

const formatTime = (seconds: number | undefined, locale: string) => {
  if (!seconds) return "";
  try {
    return new Date(seconds * 1000).toLocaleString(locale === "en" ? "en-US" : "zh-CN");
  } catch {
    return "";
  }
};

// RuleSetsPanel manages the subscribed domain-category lists (issue #62): each
// list is one row bound to a single outbound, so a whole category can be routed
// without typing its domains one by one.
export function RuleSetsPanel({
  outbounds,
  preview = false,
  onChanged,
}: {
  preview?: boolean;
  onChanged?: () => void;
  outbounds: RuleSetOutboundOption[];
}) {
  const { t, locale } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const [selectedID, setSelectedID] = useState("");
  const contentID = useId();
  const contentPanel = useRef<HTMLElement>(null);
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(0);
  const [content, setContent] = useState<RuleSetEntries | null>(null);
  const [contentError, setContentError] = useState("");
  const [contentLoading, setContentLoading] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<RuleSet | null>(null);
  const [deleteError, setDeleteError] = useState("");
  const [sets, setSets] = useState<RuleSet[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [busyId, setBusyId] = useState("");
  const [draft, setDraft] = useState<DraftRuleSet>(emptyDraft);
  const [notice, setNotice] = useState<{ intent: "success" | "error"; message: string } | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setLoadFailed(false);
    setLoadError("");
    try {
      setSets(preview ? [{ id: "preview-steam", name: "Steam · 示例", url: "https://rules.example.com/steam.yaml", outbound: "direct", entry_count: 3, format: "clash-provider" }] : (await appServices.ruleSets.list()) ?? []);
    } catch (reason) {
      setLoadFailed(true);
      setLoadError(String(reason));
    } finally {
      setLoading(false);
    }
  }, [preview]);

  useEffect(() => { void load(); }, [load]);

  const selected = sets.find((set) => set.id === selectedID);
  useEffect(() => {
    if (selectedID) contentPanel.current?.scrollIntoView?.({ block: "nearest" });
  }, [selectedID]);
  useEffect(() => {
    if (!selected) { setContent(null); return; }
    let cancelled = false;
    setContent(null);
    setContentError("");
    setContentLoading(true);
    const timer = window.setTimeout(() => {
      const example = ["steampowered.com", "steamcommunity.com", "steamcontent.com"].filter((value) => value.includes(query.toLowerCase().trim()));
      const request = preview
        ? Promise.resolve({ entries: example.map((value) => ({ kind: "domain_suffix", value })), total: example.length, downloaded: true })
        : appServices.ruleSets.entries(selected.id, query, page * 100, 100);
      request.then((result) => { if (!cancelled) setContent(result); })
        .catch((reason) => { if (!cancelled) setContentError(String(reason)); })
        .finally(() => { if (!cancelled) setContentLoading(false); });
    }, 180);
    return () => { cancelled = true; window.clearTimeout(timer); };
  }, [selected, query, page, preview]);
  const chooseSet = (id: string) => { setSelectedID(current => current === id ? "" : id); setQuery(""); setPage(0); };
  const kindLabel = (kind: string) => ({ domain: text("完整域名", "Domain"), domain_suffix: text("域名后缀", "Domain suffix"), domain_keyword: text("域名关键词", "Domain keyword"), domain_regex: text("域名正则", "Domain regex"), ip_cidr: "IP / CIDR" }[kind] ?? kind);

  const save = useCallback(async (next: RuleSet[]) => {
    const saved = (await appServices.ruleSets.save(next)) ?? [];
    setSets(saved);
    onChanged?.();
  }, [onChanged]);

  const priorityValid = /^\d+$/.test(draft.priority) && Number(draft.priority) <= 999;

  const addSet = useCallback(async () => {
    if (loading || loadFailed || busyId || !priorityValid || !draft.name.trim() || !draft.url.trim()) return;
    setBusyId("draft");
    setNotice(null);
    try {
      await save([
        ...sets,
        {
          id: "", // The service mints an identifier for new rows.
          name: draft.name.trim(),
          url: draft.url.trim(),
          outbound: draft.outbound,
          priority: Number(draft.priority),
        },
      ]);
      setDraft(emptyDraft);
      setNotice({ intent: "success", message: t("rulesets_save_success") });
    } catch (reason) {
      setNotice({ intent: "error", message: t("rulesets_save_failed").replace("{error}", String(reason)) });
    } finally {
      setBusyId("");
    }
  }, [draft, save, sets, t, loading, loadFailed, busyId, priorityValid]);

  const updateSet = useCallback(async (set: RuleSet) => {
    setBusyId(set.id);
    setNotice(null);
    try {
      const saved = (await appServices.ruleSets.update(set.id)) ?? [];
      setSets(saved);
      onChanged?.();
      setSelectedID(set.id);
      setPage(0);
      setNotice({ intent: "success", message: t("rulesets_update_success") });
    } catch (reason) {
      setNotice({ intent: "error", message: t("rulesets_update_failed").replace("{error}", String(reason)) });
      await load();
    } finally {
      setBusyId("");
    }
  }, [load, t, onChanged]);

  const toggleSet = useCallback(async (set: RuleSet) => {
    setBusyId(set.id);
    setNotice(null);
    try {
      await save(sets.map((candidate) => (candidate.id === set.id ? { ...candidate, disabled: !candidate.disabled } : candidate)));
    } catch (reason) {
      setNotice({ intent: "error", message: t("rulesets_save_failed").replace("{error}", String(reason)) });
    } finally {
      setBusyId("");
    }
  }, [save, sets, t]);

  const removeSet = useCallback(async (set: RuleSet) => {
    setDeleteError("");
    setBusyId(set.id);
    setNotice(null);
    try {
      await save(sets.filter((candidate) => candidate.id !== set.id));
      setDeleteTarget(null);
      setQuery("");
      setPage(0);
    } catch (reason) {
      setDeleteError(String(reason));
    } finally {
      setBusyId("");
    }
  }, [save, sets, t]);

  return (
    <div className="routing-rulesets-surface">
      <div className="routing-rulesets-heading">
        <h2>{text("订阅管理", "Subscription management")}</h2>
        <p>{t("rulesets_hint")}</p>
      </div>
      {preview && <MessageBar intent="info"><MessageBarBody>{text("浏览器预览 · 以下为示例规则，未连接桌面服务。", "Browser preview · Sample rules only; desktop service is disconnected.")}</MessageBarBody></MessageBar>}
      {notice && (
        <MessageBar intent={notice.intent}>
          <MessageBarBody>{notice.message}</MessageBarBody>
        </MessageBar>
      )}
      <GlassSurface className="ruleset-add-section" tone="secondary">
      <h3 className="ruleset-add-title">{text("添加订阅", "Add subscription")}</h3>
      <div className="routing-rulesets-add">
        <Field label={t("rulesets_col_name")}><Input
          aria-label={t("rulesets_col_name")}
          placeholder={t("rulesets_name_placeholder")}
          value={draft.name}
          onChange={(_, data) => setDraft({ ...draft, name: data.value })}
        /></Field>
        <Field className="routing-rulesets-url" label={t("rulesets_col_url")}><Input
          type="url"
          aria-label={t("rulesets_col_url")}
          placeholder={t("rulesets_url_placeholder")}
          value={draft.url}
          onChange={(_, data) => setDraft({ ...draft, url: data.value })}
        /></Field>
        <Field label={t("rulesets_col_outbound")}><Dropdown
          aria-label={t("rulesets_col_outbound")}
          value={(outbounds.find((option) => option.id === draft.outbound)?.label) ?? draft.outbound}
          selectedOptions={[draft.outbound]}
          onOptionSelect={(_, data) => data.optionValue && setDraft({ ...draft, outbound: String(data.optionValue) })}
        >
          {outbounds.map((option) => (
            <Option key={option.id} value={option.id}>{option.label}</Option>
          ))}
        </Dropdown></Field>
        <Field label={text("优先级", "Priority")} validationState={priorityValid ? "none" : "error"} validationMessage={priorityValid ? undefined : text("请输入 0–999 的整数", "Enter an integer from 0 to 999")}><Input
          inputMode="numeric"
          aria-label={text("优先级", "Priority")}
          placeholder={t("rulesets_priority").replace("{priority}", "0–999")}
          value={draft.priority}
          onChange={(_, data) => setDraft({ ...draft, priority: data.value })}
        /></Field>
        <Button
          appearance="primary"
          icon={busyId === "draft" ? <Spinner size="tiny" /> : <Add20Regular />}
          disabled={preview || loading || loadFailed || !priorityValid || busyId !== "" || !draft.name.trim() || !draft.url.trim()}
          onClick={() => void addSet()}
        >{t("rulesets_add")}</Button>
      </div>
      <p className="routing-rulesets-note">{t("rulesets_restart_hint")}</p>
      </GlassSurface>
      <div className="ruleset-list-heading"><h3>{text("已添加的订阅", "Your subscriptions")}</h3><span>{text(`${sets.length} 个订阅`, `${sets.length} subscriptions`)}</span></div>
      {loading ? (
        <Spinner label={t("rulesets_loading")} />
      ) : loadFailed ? (
        <MessageBar intent="error"><MessageBarBody>{loadError} <Button onClick={() => void load()}>{text("重新读取规则集", "Retry loading rule sets")}</Button></MessageBarBody></MessageBar>
      ) : sets.length === 0 ? (
        <p className="routing-rulesets-empty">{t("rulesets_empty")}</p>
      ) : (
        <ul className="routing-rulesets-list">
          {sets.map((set) => (
            <li key={set.id} className={`routing-rulesets-row${set.disabled ? " is-disabled" : ""}`}>
              <div className="routing-rulesets-main">
                <div className="ruleset-title-line"><span className="routing-rulesets-name">{set.name}</span><Badge appearance="tint" color={set.disabled ? "subtle" : "success"}>{set.disabled ? t("rulesets_disabled") : text("已启用", "Enabled")}</Badge></div>
                <span className="ruleset-source">{set.url}</span>
                <span className="routing-rulesets-meta">
                  {(outbounds.find((option) => option.id === set.outbound)?.label) ?? set.outbound.replace(/^nic_/, "")}
                  {" · "}
                  {t("rulesets_priority").replace("{priority}", String(set.priority ?? 0))}
                  {set.entry_count !== undefined
                    ? ` · ${t("rulesets_entries").replace("{count}", String(set.entry_count)).replace("{format}", set.format ?? "")}${set.ignored_count ? ` · ${t("rulesets_ignored").replace("{count}", String(set.ignored_count))}` : ""}`
                    : ` · ${t("rulesets_never_fetched")}`}
                  {set.updated_at ? ` · ${formatTime(set.updated_at, locale)}` : ""}
                </span>
                {set.last_error && (
                  <span className="routing-rulesets-error" title={set.last_error}>{set.last_error}</span>
                )}
              </div>
              <div className="routing-rulesets-actions">
                <Button size="small" icon={selected?.id === set.id ? <ChevronDown16Regular /> : <ChevronRight16Regular />} appearance="subtle" aria-expanded={selected?.id === set.id} aria-controls={selected?.id === set.id ? contentID : undefined} onClick={() => chooseSet(set.id)}>{selected?.id === set.id ? text("收起内容", "Hide contents") : text("查看内容", "View contents")}</Button>
                <Button
                  size="small"
                  icon={busyId === set.id ? <Spinner size="tiny" /> : <ArrowClockwise20Regular />}
                  disabled={preview || busyId !== ""}
                  onClick={() => void updateSet(set)}
                >{t("rulesets_update_now")}</Button>
                <Button
                  size="small"
                  disabled={preview || busyId !== ""}
                  onClick={() => void toggleSet(set)}
                >{set.disabled ? t("rulesets_enable") : t("rulesets_disable")}</Button>
                <Button
                  size="small"
                  appearance="subtle"
                  icon={busyId === set.id ? <Spinner size="tiny" /> : <Delete16Regular />}
                  disabled={busyId !== ""}
                  onClick={() => { setDeleteError(""); setDeleteTarget(set); }}
                >{t("rulesets_delete")}</Button>
              </div>
      {selected?.id === set.id && <section id={contentID} ref={contentPanel} className="ruleset-content" aria-label={text("规则集内容", "Rule set contents")}>
        <div className="ruleset-content-heading"><div><p>{text("已下载并解析的条目；实际出口还受手动规则和优先级影响。", "Downloaded entries; manual rules and priorities also affect routing.")}</p></div>
          <Badge appearance="outline">{selected.disabled ? t("rulesets_disabled") : text("已启用", "Enabled")}</Badge>
        </div>
        <div className="ruleset-content-tools">
          <SearchBox aria-label={text("搜索规则集内容", "Search rule set contents")} placeholder={text("搜索域名或 IP", "Search domains or IPs")} value={query} onChange={(_, data) => { setQuery(data.value); setPage(0); }} />
          <span>{content ? text(`${content.total} 条匹配`, `${content.total} matches`) : ""}</span>
        </div>
        {contentLoading ? <Spinner label={text("正在读取规则内容", "Loading rule contents")} />
          : contentError ? <MessageBar intent="error"><MessageBarBody>{contentError}</MessageBarBody></MessageBar>
          : !content?.downloaded ? <p className="ruleset-content-empty">{text("订阅地址已保存，尚无本地规则内容。点击「立即更新」下载后即可查看。", "Subscription saved, but no local rules are available. Choose Update now to download them.")}</p>
          : content.entries.length === 0 ? <p className="ruleset-content-empty">{text("没有匹配的规则。", "No matching rules.")}</p>
          : <div className="ruleset-entry-scroll" tabIndex={0} role="region" aria-label={text("已下载规则", "Downloaded rules")}><table className="ruleset-entry-table"><thead><tr><th>{text("匹配类型", "Match type")}</th><th>{text("匹配内容", "Match value")}</th></tr></thead><tbody>{content.entries.map((entry) => <tr key={`${entry.kind}:${entry.value}`}><td>{kindLabel(entry.kind)}</td><td><code>{entry.value}</code></td></tr>)}</tbody></table></div>}
        {content && content.total > 100 && <div className="ruleset-pagination"><Button disabled={page === 0 || contentLoading} onClick={() => setPage(page - 1)}>{text("上一页", "Previous")}</Button><span>{page + 1} / {Math.ceil(content.total / 100)}</span><Button disabled={(page + 1) * 100 >= content.total || contentLoading} onClick={() => setPage(page + 1)}>{text("下一页", "Next")}</Button></div>}
      </section>}
            </li>
          ))}
        </ul>
      )}
      <Dialog open={deleteTarget !== null} onOpenChange={(_, data) => { if (!data.open && !busyId) setDeleteTarget(null); }}>
        <DialogSurface className="glass-surface" data-tone="primary"><DialogBody>
          <DialogTitle>{text("删除规则集？", "Delete rule set?")}</DialogTitle>
          <DialogContent><p>{t("rulesets_delete_confirm").replace("{name}", deleteTarget?.name ?? "")}</p>{deleteError && <MessageBar intent="error"><MessageBarBody>{deleteError}</MessageBarBody></MessageBar>}</DialogContent>
          <DialogActions><Button disabled={busyId !== ""} onClick={() => setDeleteTarget(null)}>{text("取消", "Cancel")}</Button><Button appearance="primary" disabled={preview || busyId !== ""} onClick={() => deleteTarget && void removeSet(deleteTarget)}>{text("确认删除", "Delete subscription")}</Button></DialogActions>
        </DialogBody></DialogSurface>
      </Dialog>
    </div>
  );
}
