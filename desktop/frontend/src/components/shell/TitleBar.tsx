import {
  Button,
  Tooltip,
} from "@fluentui/react-components";
import {
  Dismiss16Regular,
  LineHorizontal120Regular,
  Maximize16Regular,
  WeatherMoon20Regular,
  WeatherSunny20Regular,
} from "@fluentui/react-icons";
import { desktopPlatform } from "../../platform/desktop";
import { useAppearance } from "../../theme/appearance.store";
import { ProductMark } from "./ProductMark";
import { useI18n } from "../../i18n/i18n";
import { productInfo } from "../../product";

export function TitleBar() {
  const { resolvedMode, update } = useAppearance();
  const { locale } = useI18n();
  const nextMode = resolvedMode === "dark" ? "light" : "dark";
  const modeLabel = locale === "en"
    ? `Switch to ${nextMode} mode`
    : `切换到${nextMode === "dark" ? "深色" : "浅色"}模式`;

  return (
    <header className="titlebar">
      <div className="titlebar-identity">
        <ProductMark />
        <strong>HypoMux</strong>
        <span>{productInfo.version} · Desktop</span>
      </div>
      <div className="titlebar-drag" />
      <div className="titlebar-actions">
        <Tooltip content={modeLabel} relationship="label">
          <Button
            appearance="subtle"
            icon={resolvedMode === "dark" ? <WeatherSunny20Regular /> : <WeatherMoon20Regular />}
            aria-label={modeLabel}
            onClick={() => update({ mode: nextMode })}
          />
        </Tooltip>
        <button className="window-button" aria-label={locale === "en" ? "Minimize" : "最小化"} onClick={() => desktopPlatform.minimise()}>
          <LineHorizontal120Regular />
        </button>
        <button className="window-button" aria-label={locale === "en" ? "Maximize" : "最大化"} onClick={() => desktopPlatform.toggleMaximise()}>
          <Maximize16Regular />
        </button>
        <button className="window-button window-close" aria-label={locale === "en" ? "Close" : "关闭窗口"} onClick={() => desktopPlatform.close()}>
          <Dismiss16Regular />
        </button>
      </div>
    </header>
  );
}
