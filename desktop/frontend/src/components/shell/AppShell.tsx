import type { PropsWithChildren, ReactNode } from "react";
import { useCardGlowField } from "../material/useCardGlowField";
import { CompactNavigation, type AppPage } from "./CompactNavigation";
import { TitleBar } from "./TitleBar";

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

  return (
    <div className="app-shell">
      <TitleBar />
      <CompactNavigation page={page} onPageChange={onPageChange} />
      <div className="page-viewport">
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
