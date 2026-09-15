import { desktopPlatform } from "../../platform/desktop";

export function TrayMenu() {
  return <main className="tray-menu"><section className="tray-menu__card" role="menu" aria-label="HypoMux 托盘菜单">
    <header className="tray-menu__header"><div className="tray-menu__title">HypoMux</div><div className="tray-menu__status">聚合引擎状态</div></header>
    <button className="tray-menu__action" role="menuitem" onClick={() => desktopPlatform.show()}>显示主窗口</button>
    <button className="tray-menu__action" role="menuitem" onClick={() => desktopPlatform.hideToTray()}>隐藏到托盘</button>
    <button className="tray-menu__action" role="menuitem" onClick={() => desktopPlatform.quit()}>退出 HypoMux</button>
  </section></main>;
}
