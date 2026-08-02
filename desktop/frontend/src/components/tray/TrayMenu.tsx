import { useEffect, useState } from "react";
import { Events } from "@wailsio/runtime";
import { desktopPlatform } from "../../platform/desktop";
import "./TrayMenu.css";

interface EngineStatus {
  phase: string;
  mode: string;
}

export function TrayMenu() {
  const [status, setStatus] = useState<EngineStatus>({ phase: "stopped", mode: "system" });

  useEffect(() => {
    // 监听引擎状态更新
    const unsubscribe = Events.On("engine:status", (ev) => {
      if (ev.data && typeof ev.data === 'object' && 'phase' in ev.data && 'mode' in ev.data) {
        setStatus(ev.data as EngineStatus);
      }
    });

    return () => unsubscribe();
  }, []);

  const getStatusText = () => {
    switch (status.phase) {
      case "running":
        return "运行中";
      case "degraded":
        return "降级运行";
      case "starting":
        return "正在启动";
      case "stopping":
        return "正在停止";
      case "failed":
        return "异常";
      default:
        return "未启动";
    }
  };

  const getModeText = () => {
    return status.mode === "tun" ? "虚拟网卡" : "系统代理";
  };

  const getStatusColor = () => {
    switch (status.phase) {
      case "running":
        return "#107c10";
      case "degraded":
      case "failed":
        return "#d97706";
      case "starting":
      case "stopping":
        return "#0f6cbd";
      default:
        return "#667085";
    }
  };

  const handleShow = () => {
    void desktopPlatform.show();
    void desktopPlatform.hideCurrentWindow();
  };

  const handleHide = () => {
    void desktopPlatform.hideMainToTray();
    void desktopPlatform.hideCurrentWindow();
  };

  const handleQuit = () => {
    void desktopPlatform.quit();
  };

  return (
    <div className="tray-menu-container">
      <div className="tray-menu-status">
        <div
          className="status-indicator"
          style={{ backgroundColor: getStatusColor(), color: getStatusColor() }}
        />
        <div className="status-text">
          <div className="status-label">聚合引擎</div>
          <div className="status-value">{getStatusText()} · {getModeText()}</div>
        </div>
      </div>

      <div className="tray-menu-separator" />

      <button className="tray-menu-item" onClick={handleShow}>
        <svg className="tray-menu-icon" viewBox="0 0 20 20" fill="currentColor">
          <path d="M10 3a1 1 0 011 1v5h5a1 1 0 110 2h-5v5a1 1 0 11-2 0v-5H4a1 1 0 110-2h5V4a1 1 0 011-1z" />
        </svg>
        <span>打开 HypoMux</span>
      </button>

      <button className="tray-menu-item" onClick={handleHide}>
        <svg className="tray-menu-icon" viewBox="0 0 20 20" fill="currentColor">
          <path fillRule="evenodd" d="M3.707 2.293a1 1 0 00-1.414 1.414l14 14a1 1 0 001.414-1.414l-1.473-1.473A10.014 10.014 0 0019.542 10C18.268 5.943 14.478 3 10 3a9.958 9.958 0 00-4.512 1.074l-1.78-1.781zm4.261 4.26l1.514 1.515a2.003 2.003 0 012.45 2.45l1.514 1.514a4 4 0 00-5.478-5.478z" clipRule="evenodd" />
          <path d="M12.454 16.697L9.75 13.992a4 4 0 01-3.742-3.741L2.335 6.578A9.98 9.98 0 00.458 10c1.274 4.057 5.065 7 9.542 7 .847 0 1.669-.105 2.454-.303z" />
        </svg>
        <span>隐藏窗口</span>
      </button>

      <div className="tray-menu-separator" />

      <button className="tray-menu-item tray-menu-item-danger" onClick={handleQuit}>
        <svg className="tray-menu-icon" viewBox="0 0 20 20" fill="currentColor">
          <path fillRule="evenodd" d="M3 3a1 1 0 00-1 1v12a1 1 0 102 0V4a1 1 0 00-1-1zm10.293 9.293a1 1 0 001.414 1.414l3-3a1 1 0 000-1.414l-3-3a1 1 0 10-1.414 1.414L14.586 9H7a1 1 0 100 2h7.586l-1.293 1.293z" clipRule="evenodd" />
        </svg>
        <span>退出</span>
      </button>
    </div>
  );
}
