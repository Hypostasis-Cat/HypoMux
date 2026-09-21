import type { PropsWithChildren, ReactNode } from "react";
import { useCardGlowField } from "../material/useCardGlowField";
import { CompactNavigation, type AppPage } from "./CompactNavigation";
import { TitleBar } from "./TitleBar";
import { useI18n } from "../../i18n/i18n";

export function AppShell({
  page,
  onPageChange,
  pageDirection,
  animatePage,
  persistentPage,
  persistentChildren,
  children,
}: PropsWithChildren<{
  page: AppPage;
  onPageChange: (page: AppPage) => void;
  pageDirection: "forward" | "backward";
  animatePage: boolean;
  persistentPage?: AppPage;
  persistentChildren?: ReactNode;
}>) {
  useCardGlowField();
  const { locale } = useI18n();

  return (
    <div className="app-shell">
      <a className="skip-to-content" href="#page-content" onClick={(event) => {
        event.preventDefault();
        document.getElementById("page-content")?.focus();
      }}>{locale === "en" ? "Skip to content" : "跳转到页面内容"}</a>
      <TitleBar />
      <CompactNavigation page={page} onPageChange={onPageChange} />
      <div id="page-content" className="page-viewport" tabIndex={-1}>
        {persistentPage && persistentChildren ? (
          <div
            className={`page-transition-layer${page === persistentPage && animatePage ? " is-entering" : ""}`}
            data-direction={pageDirection}
            hidden={page !== persistentPage}
          >
            {persistentChildren}
          </div>
        ) : null}
        {page !== persistentPage ? (
          <div
            key={page}
            className={`page-transition-layer${animatePage ? " is-entering" : ""}`}
            data-direction={pageDirection}
          >
            {children}
          </div>
        ) : null}
      </div>
    </div>
  );
}
