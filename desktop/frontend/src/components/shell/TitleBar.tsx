import { Button, Tooltip } from "@fluentui/react-components";
import {
  Dismiss16Regular,
  Square16Regular,
  SquareMultiple16Regular,
  Subtract16Regular,
  WeatherMoon20Regular,
  WeatherSunny20Regular,
} from "@fluentui/react-icons";
import { Events } from "@wailsio/runtime";
import { useCallback, useEffect, useState } from "react";
import { useI18n } from "../../i18n/i18n";
import { desktopPlatform } from "../../platform/desktop";
import { productInfo } from "../../product";
import { useAppearance } from "../../theme/appearance.store";
import { ProductMark } from "./ProductMark";

export function TitleBar() {
  const { resolvedMode, update } = useAppearance();
  const { locale } = useI18n();
  const [maximised, setMaximised] = useState(false);
  const nextMode = resolvedMode === "dark" ? "light" : "dark";
  const modeLabel = locale === "en"
    ? `Switch to ${nextMode} mode`
    : `切换到${nextMode === "dark" ? "深色" : "浅色"}模式`;

  const refreshWindowState = useCallback(() => {
    void desktopPlatform.isMaximised().then(setMaximised);
  }, []);

  useEffect(() => {
    refreshWindowState();
    const offMaximise = Events.On("common:WindowMaximise", () => setMaximised(true));
    const offRestore = Events.On("common:WindowUnMaximise", () => setMaximised(false));
    return () => {
      offMaximise();
      offRestore();
    };
  }, [refreshWindowState]);

  const minimiseLabel = locale === "en" ? "Minimize" : "最小化";
  const maximiseLabel = locale === "en"
    ? maximised ? "Restore" : "Maximize"
    : maximised ? "还原窗口" : "最大化";
  const closeLabel = locale === "en" ? "Close" : "关闭窗口";

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
        <Tooltip content={minimiseLabel} relationship="label">
          <Button
            appearance="subtle"
            className="window-button"
            icon={<Subtract16Regular />}
            aria-label={minimiseLabel}
            onClick={() => desktopPlatform.minimise()}
          />
        </Tooltip>
        <Tooltip content={maximiseLabel} relationship="label">
          <Button
            appearance="subtle"
            className="window-button"
            icon={maximised ? <SquareMultiple16Regular /> : <Square16Regular />}
            aria-label={maximiseLabel}
            onClick={() => void desktopPlatform.toggleMaximise().finally(refreshWindowState)}
          />
        </Tooltip>
        <Tooltip content={closeLabel} relationship="label">
          <Button
            appearance="subtle"
            className="window-button window-close"
            icon={<Dismiss16Regular />}
            aria-label={closeLabel}
            onClick={() => desktopPlatform.close()}
          />
        </Tooltip>
      </div>
    </header>
  );
}
