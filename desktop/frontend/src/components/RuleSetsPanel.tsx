import { Badge, Button, Dropdown, Input, MessageBar, MessageBarBody, Option, Spinner } from "@fluentui/react-components";
import { ArrowClockwise20Regular, Add20Regular, Delete16Regular } from "@fluentui/react-icons";
import { useCallback, useEffect, useState } from "react";
import { appServices, type RuleSet } from "../platform/services";
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
}: {
  outbounds: RuleSetOutboundOption[];
}) {
  const { t, locale } = useI18n();
  const [sets, setSets] = useState<RuleSet[]>([]);
  const [loading, setLoading] = useState(true);
  const [busyId, setBusyId] = useState("");
  const [draft, setDraft] = useState<DraftRuleSet>(emptyDraft);
  const [notice, setNotice] = useState<{ intent: "success" | "error"; message: string } | null>(null);

  const load = useCallback(async () => {
    try {
      setSets(await appServices.ruleSets.list());
    } catch (reason) {
      setNotice({ intent: "error", message: String(reason) });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const save = useCallback(async (next: RuleSet[]) => {
    const saved = await appServices.ruleSets.save(next);
    setSets(saved);
  }, []);

  const addSet = useCallback(async () => {
    if (!draft.name.trim() || !draft.url.trim()) return;
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
          priority: Math.max(0, Math.min(999, Number.parseInt(draft.priority || "0", 10) || 0)),
        },
      ]);
      setDraft(emptyDraft);
      setNotice({ intent: "success", message: t("rulesets_save_success") });
    } catch (reason) {
      setNotice({ intent: "error", message: t("rulesets_save_failed").replace("{error}", String(reason)) });
    } finally {
      setBusyId("");
    }
  }, [draft, save, sets, t]);

  const updateSet = useCallback(async (set: RuleSet) => {
    setBusyId(set.id);
    setNotice(null);
    try {
      const saved = await appServices.ruleSets.update(set.id);
      setSets(saved);
      setNotice({ intent: "success", message: t("rulesets_update_success") });
    } catch (reason) {
      setNotice({ intent: "error", message: t("rulesets_update_failed").replace("{error}", String(reason)) });
      void load();
    } finally {
      setBusyId("");
    }
  }, [load, t]);

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
    if (!window.confirm(t("rulesets_delete_confirm").replace("{name}", set.name))) return;
    setBusyId(set.id);
    setNotice(null);
    try {
      await save(sets.filter((candidate) => candidate.id !== set.id));
    } catch (reason) {
      setNotice({ intent: "error", message: t("rulesets_save_failed").replace("{error}", String(reason)) });
    } finally {
      setBusyId("");
    }
  }, [save, sets, t]);

  return (
    <GlassSurface className="routing-rulesets-surface" tone="secondary">
      <div className="routing-rulesets-heading">
        <h2>{t("rulesets_title")}</h2>
        <p>{t("rulesets_hint")}</p>
      </div>
      {notice && (
        <MessageBar intent={notice.intent}>
          <MessageBarBody>{notice.message}</MessageBarBody>
        </MessageBar>
      )}
      {loading ? (
        <Spinner label={t("rulesets_loading")} />
      ) : sets.length === 0 ? (
        <p className="routing-rulesets-empty">{t("rulesets_empty")}</p>
      ) : (
        <ul className="routing-rulesets-list">
          {sets.map((set) => (
            <li key={set.id} className={`routing-rulesets-row${set.disabled ? " is-disabled" : ""}`}>
              <div className="routing-rulesets-main">
                <span className="routing-rulesets-name">{set.name}</span>
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
                {set.disabled && <Badge appearance="outline">{t("rulesets_disabled")}</Badge>}
                <Button
                  size="small"
                  icon={busyId === set.id ? <Spinner size="tiny" /> : <ArrowClockwise20Regular />}
                  disabled={busyId !== ""}
                  onClick={() => void updateSet(set)}
                >{t("rulesets_update_now")}</Button>
                <Button
                  size="small"
                  disabled={busyId !== ""}
                  onClick={() => void toggleSet(set)}
                >{set.disabled ? t("rulesets_enable") : t("rulesets_disable")}</Button>
                <Button
                  size="small"
                  appearance="subtle"
                  icon={busyId === set.id ? <Spinner size="tiny" /> : <Delete16Regular />}
                  disabled={busyId !== ""}
                  onClick={() => void removeSet(set)}
                >{t("rulesets_delete")}</Button>
              </div>
            </li>
          ))}
        </ul>
      )}
      <div className="routing-rulesets-add">
        <Input
          aria-label={t("rulesets_col_name")}
          placeholder={t("rulesets_name_placeholder")}
          value={draft.name}
          onChange={(_, data) => setDraft({ ...draft, name: data.value })}
        />
        <Input
          className="routing-rulesets-url"
          aria-label={t("rulesets_col_url")}
          placeholder={t("rulesets_url_placeholder")}
          value={draft.url}
          onChange={(_, data) => setDraft({ ...draft, url: data.value })}
        />
        <Dropdown
          aria-label={t("rulesets_col_outbound")}
          value={(outbounds.find((option) => option.id === draft.outbound)?.label) ?? draft.outbound}
          selectedOptions={[draft.outbound]}
          onOptionSelect={(_, data) => data.optionValue && setDraft({ ...draft, outbound: String(data.optionValue) })}
        >
          {outbounds.map((option) => (
            <Option key={option.id} value={option.id}>{option.label}</Option>
          ))}
        </Dropdown>
        <Input
          className="routing-rulesets-priority"
          aria-label={t("rulesets_priority")}
          placeholder={t("rulesets_priority").replace("{priority}", "0–999")}
          value={draft.priority}
          onChange={(_, data) => setDraft({ ...draft, priority: data.value })}
        />
        <Button
          appearance="primary"
          icon={busyId === "draft" ? <Spinner size="tiny" /> : <Add20Regular />}
          disabled={busyId !== "" || !draft.name.trim() || !draft.url.trim()}
          onClick={() => void addSet()}
        >{t("rulesets_add")}</Button>
      </div>
      <p className="routing-rulesets-note">{t("rulesets_restart_hint")}</p>
    </GlassSurface>
  );
}
