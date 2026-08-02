import { FluentProvider, webLightTheme } from "@fluentui/react-components";
import { useEffect } from "react";
import { TrayMenu } from "./components/tray/TrayMenu";
import { desktopPlatform } from "./platform/desktop";
import "./theme/design.tokens.css";
import "./theme/material.tokens.css";
import "./theme/semantic.tokens.css";
import "./theme/typography.tokens.css";
import "./theme/motion.tokens.css";
import "./app.css";

function TrayMenuWindow() {
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        void desktopPlatform.hideCurrentWindow();
      }
    };
    document.addEventListener("keydown", handleKeyDown);

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, []);

  return (
    <FluentProvider theme={webLightTheme} className="hypomux-provider tray-menu-provider">
      <TrayMenu />
    </FluentProvider>
  );
}

export function TrayMenuApp() {
  return <TrayMenuWindow />;
}
