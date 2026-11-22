import { Badge, Button, Dropdown, Input, Option, Spinner, Tooltip } from "@fluentui/react-components";
import { Delete24Regular, Pause24Regular, Play24Regular } from "@fluentui/react-icons";
import { useCallback, useEffect, useState } from "react";
import { GlassSurface } from "../components/material/GlassSurface";
import { PageHeader } from "../components/shell/PageHeader";
import { useAppNotifications } from "../components/notifications/AppNotifications";
import { useI18n } from "../i18n/i18n";
import { appServices, type DownloadProbe, type DownloadTask } from "../platform/services";
import "./downloader.css";

const statusBadge: Record<DownloadTask["status"], "filled" | "tint" | "outline" | "ghost"> = {
  running: "filled",
  merging: "filled",
  completed: "tint",
  paused: "outline",
  error: "tint",
  cancelled: "outline",
};

function human(bytes: number): string {
  if (bytes < 1024) return `${Math.round(bytes)} B`;
  let value = bytes;
  for (const unit of ["KB", "MB", "GB", "TB"]) {
    value /= 1024;
    if (value < 1024) return `${value.toFixed(1)} ${unit}`;
  }
  return `${value.toFixed(1)} PB`;
}

export function DownloaderPage() {
  const { locale } = useI18n();
  const text = (zh: string, en: string) => (locale === "en" ? en : zh);
  const { notify } = useAppNotifications();

  const [url, setUrl] = useState("");
  const [output, setOutput] = useState("");
  const [segments, setSegments] = useState(16);
  const [probe, setProbe] = useState<DownloadProbe>();
  const [tasks, setTasks] = useState<DownloadTask[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const hasActive = tasks.some(item => item.status === "running" || item.status === "merging");

  const refresh = useCallback(async () => {
    try {
      const list = await appServices.downloader.list();
      setTasks(list);
    } catch {
      // 服务暂时不可达时保持上次快照
    }
  }, []);

  useEffect(() => {
    if (!hasActive) return;
    const timer = setInterval(() => void refresh(), 800);
    return () => clearInterval(timer);
  }, [hasActive, refresh]);

  useEffect(() => { void refresh(); }, [refresh]);

  const doProbe = async () => {
    if (!url.trim()) return;
    setBusy(true);
    setError("");
    try {
      const result = await appServices.downloader.probe(url.trim());
      setProbe(result);
      setOutput(current => current || result.file_name);
    } catch (err) {
      setProbe(undefined);
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const doStart = async () => {
    setBusy(true);
    setError("");
    try {
      const task = await appServices.downloader.start(url.trim(), output.trim(), segments);
      setProbe(undefined);
      setUrl("");
      setOutput("");
      void refresh();
      notify({ title: text("下载已开始", "Download started"), message: `${task.output} × ${task.segments}`, intent: "success" });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  const control = async (id: string, action: "pause" | "resume" | "remove") => {
    try {
      if (action === "remove") await appServices.downloader.remove(id);
      else await appServices.downloader[action](id);
      void refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="downloader-page">
      <PageHeader
        title={text("多线程下载", "Multi-thread Download")}
        kicker={text("多网卡聚合加速", "Multi-adapter acceleration")}
        description={text(
          "把大文件切成多段并发下载；开启聚合引擎后，各分段连接将自动分配到不同网卡，实现多链路叠加提速。",
          "Splits a large file into concurrent range requests. With the aggregation engine on, segments are spread across adapters for combined throughput.",
        )}
      />
      <GlassSurface className="downloader-panel">
        <div className="downloader-form">
          <Input
            className="downloader-url"
            placeholder={text("下载地址（https://…）", "Download URL (https://…)")}
            value={url}
            onChange={(_, data) => { setUrl(data.value); setProbe(undefined); }}
            contentAfter={busy ? <Spinner size="tiny" /> : undefined}
          />
          <div className="downloader-row">
            <Input
              className="downloader-output"
              placeholder={text("保存文件名", "Output file name")}
              value={output}
              onChange={(_, data) => setOutput(data.value)}
            />
            <Dropdown
              className="downloader-segments"
              value={String(segments)}
              selectedOptions={[String(segments)]}
              onOptionSelect={(_, data) => setSegments(Number(data.optionValue ?? 16))}
            >
              {[1, 4, 8, 16, 24, 32, 64].map(value => (
                <Option key={value} value={String(value)}>{text(`${value} 连接`, `${value} connections`)}</Option>
              ))}
            </Dropdown>
            {probe
              ? (
                <Button appearance="primary" disabled={busy} onClick={() => void doStart()}>
                  {text("开始下载", "Start")}
                </Button>
              )
              : (
                <Button appearance="primary" disabled={busy || !url.trim()} onClick={() => void doProbe()}>
                  {text("探测", "Probe")}
                </Button>
              )}
          </div>
          {probe && (
            <div className="downloader-probe">
              <strong>{probe.file_name}</strong>
              <span>{human(probe.size)}</span>
              <Badge appearance={probe.accept_ranges ? "tint" : "outline"}>
                {probe.accept_ranges
                  ? text("支持分段并发", "Range OK")
                  : text("不支持分段，单连接下载", "No range, single stream")}
              </Badge>
            </div>
          )}
          {error && <div className="downloader-error" role="alert">{error}</div>}
        </div>
      </GlassSurface>

      <GlassSurface className="downloader-panel">
        <h2>{text("任务列表", "Tasks")}</h2>
        {tasks.length === 0
          ? <p className="downloader-empty">{text("暂无下载任务。", "No download tasks yet.")}</p>
          : (
            <ul className="downloader-tasks">
              {tasks.map(task => {
                const pct = task.total > 0 ? Math.min(100, (task.done / task.total) * 100) : 0;
                const active = task.status === "running" || task.status === "merging";
                return (
                  <li key={task.id} className="downloader-task">
                    <div className="downloader-task-main">
                      <div className="downloader-task-title">
                        <strong title={task.output}>{task.output.split(/[\\/]/).pop()}</strong>
                        <Badge appearance={statusBadge[task.status]}>{task.status}</Badge>
                      </div>
                      <div className="downloader-task-progress">
                        <div className="downloader-bar"><span style={{ width: `${pct}%` }} /></div>
                        <span className="downloader-nums">
                          {pct.toFixed(1)}% · {human(task.done)}/{human(task.total)}
                          {active ? ` · ${human(task.speed)}/s · ×${task.segments}` : ""}
                        </span>
                      </div>
                    </div>
                    <div className="downloader-task-actions">
                      {task.status === "running" && (
                        <Tooltip content={text("暂停", "Pause")} relationship="label">
                          <Button icon={<Pause24Regular />} aria-label={text("暂停", "Pause")} onClick={() => void control(task.id, "pause")} />
                        </Tooltip>
                      )}
                      {(task.status === "paused" || task.status === "error") && (
                        <Tooltip content={text("继续", "Resume")} relationship="label">
                          <Button icon={<Play24Regular />} aria-label={text("继续", "Resume")} onClick={() => void control(task.id, "resume")} />
                        </Tooltip>
                      )}
                      <Tooltip content={text("删除任务", "Remove")} relationship="label">
                        <Button icon={<Delete24Regular />} aria-label={text("删除任务", "Remove")} onClick={() => void control(task.id, "remove")} />
                      </Tooltip>
                    </div>
                    {task.error && <p className="downloader-task-error">{task.error}</p>}
                  </li>
                );
              })}
            </ul>
          )}
      </GlassSurface>
    </div>
  );
}
