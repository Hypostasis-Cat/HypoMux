import {
  Badge,
  Button,
  Dialog,
  DialogActions,
  DialogBody,
  DialogContent,
  DialogSurface,
  DialogTitle,
  DialogTrigger,
} from "@fluentui/react-components";
import {
  ArrowLeft20Regular,
  ArrowSync20Regular,
  Delete20Regular,
  Dismiss20Regular,
} from "@fluentui/react-icons";
import { useCallback, useEffect, useRef, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { useAppNotifications } from "../components/notifications/AppNotifications";
import { appServices, type BlockedDomainSnapshot } from "../platform/services";
import { useI18n } from "../i18n/i18n";
import { startSerialPoll } from "../platform/serialPoll";

const emptySnapshot: BlockedDomainSnapshot = { enabled: false, use_expiry: true, entries: [] };

export function BlockedDomainsPage({ onBack }: { onBack: () => void }) {
  const { locale, t } = useI18n();
  const text = (zh: string, en: string) => locale === "en" ? en : zh;
  const remainingText = (seconds: number, permanent: boolean) => {
    if (permanent) return t("blocked_permanent");
    if (seconds >= 60) return t("blocked_expire_min", { min: Math.floor(seconds / 60) });
    return t("blocked_expire_sec", { sec: Math.max(0, seconds) });
  };
  const [snapshot, setSnapshot] = useState(emptySnapshot);
  const [loading, setLoading] = useState(true);
  const [clearOpen, setClearOpen] = useState(false);
  const requestSequence = useRef(0);
  const mounted = useRef(true);
  const { notify } = useAppNotifications();

  const refresh = useCallback(async () => {
    const sequence = ++requestSequence.current;
    setLoading(true);
    try {
      const next = await appServices.blockedDomains.list();
      if (mounted.current && sequence === requestSequence.current) setSnapshot(next);
    } catch (error) {
      notify({ title: text("读取域名记录失败", "Failed to load domain records"), message: String(error), intent: "error" });
    } finally {
      if (mounted.current && sequence === requestSequence.current) setLoading(false);
    }
  }, [locale, notify]);

  useEffect(() => {
    mounted.current = true;
    void refresh();
    const stop = startSerialPoll(refresh, 3000);
    return () => {
      mounted.current = false;
      requestSequence.current += 1;
      stop();
    };
  }, [refresh]);

  const remove = async (adapter: string, domain: string) => {
    try {
      await appServices.blockedDomains.remove(adapter, domain);
      await refresh();
    } catch (error) {
      notify({ title: text("移除失败", "Remove failed"), message: String(error), intent: "error" });
    }
  };

  const clear = async () => {
    try {
      await appServices.blockedDomains.clear();
      setClearOpen(false);
      await refresh();
      notify({ title: text("域名隔离记录已清空", "Domain-isolation records cleared"), intent: "success" });
    } catch (error) {
      notify({ title: text("清空失败", "Clear failed"), message: String(error), intent: "error" });
    }
  };

  return (
    <main className="blocked-domains-page">
      <header className="page-heading">
        <div>
          <span className="section-kicker">Per-adapter isolation</span>
          <h1>{t("blocked_title")}</h1>
          <p>{t("blocked_hint")}</p>
        </div>
        <Button appearance="subtle" icon={<ArrowLeft20Regular />} onClick={onBack}>{text("返回设置", "Back to settings")}</Button>
      </header>

      <GlassSurface className="blocked-domains-surface">
        <div className="blocked-toolbar">
          <div>
            <Badge appearance="outline" color={snapshot.enabled ? "success" : "subtle"}>
              {snapshot.enabled
                ? text("自动分流清单已启用", "Auto-bypass list enabled")
                : text("自动分流清单已暂停", "Auto-bypass list paused")}
            </Badge>
            <span>{t("blocked_domain_count", { count: snapshot.entries.length })}</span>
          </div>
          <div>
            <Button icon={<ArrowSync20Regular />} disabled={loading} onClick={refresh}>{t("blocked_refresh")}</Button>
            <Dialog open={clearOpen} onOpenChange={(_, data) => setClearOpen(data.open)}>
              <DialogTrigger disableButtonEnhancement>
                <Button icon={<Delete20Regular />} disabled={snapshot.entries.length === 0}>{t("blocked_clear_all")}</Button>
              </DialogTrigger>
              <DialogSurface>
                <DialogBody>
                  <DialogTitle>{text("清空全部域名隔离记录？", "Clear all domain-isolation records?")}</DialogTitle>
                  <DialogContent>{text(
                    "此操作会删除所有网卡的已确认记录，无法撤销。新的连接失败仍可能重新生成记录。",
                    "This permanently removes confirmed records for every NIC. New comparative failures may create records again.",
                  )}</DialogContent>
                  <DialogActions>
                    <DialogTrigger disableButtonEnhancement><Button>{t("routing_dialog_cancel")}</Button></DialogTrigger>
                    <Button appearance="primary" onClick={clear}>{text("确认清空", "Clear records")}</Button>
                  </DialogActions>
                </DialogBody>
              </DialogSurface>
            </Dialog>
          </div>
        </div>

        {snapshot.entries.length === 0 ? (
          <div className="blocked-empty">{loading ? text("正在加载…", "Loading…") : t("blocked_no_data")}</div>
        ) : (
          <div className="blocked-list" role="table" aria-label={t("blocked_title")}>
            <div className="blocked-list-header" role="row">
              <span role="columnheader">{t("blocked_nic_label")}</span>
              <span role="columnheader">{text("域名", "Domain")}</span>
              <span role="columnheader">{text("状态", "Status")}</span>
              <span role="columnheader" aria-label={text("操作", "Actions")} />
            </div>
            {snapshot.entries.map((entry) => (
              <div className="blocked-list-row" role="row" key={`${entry.adapter}:${entry.domain}`}>
                <strong role="cell">{entry.adapter}</strong>
                <span role="cell">{entry.domain}</span>
                <span role="cell">{remainingText(entry.remaining_seconds, entry.permanent)}</span>
                <Button
                  appearance="subtle"
                  icon={<Dismiss20Regular />}
                  aria-label={`${t("blocked_delete_domain")} ${entry.domain}`}
                  onClick={() => remove(entry.adapter, entry.domain)}
                >
                  {t("blocked_delete_domain")}
                </Button>
              </div>
            ))}
          </div>
        )}
      </GlassSurface>
    </main>
  );
}
