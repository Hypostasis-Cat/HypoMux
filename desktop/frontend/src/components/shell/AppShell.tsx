import type { PropsWithChildren } from "react";
import { useCardGlowField } from "../material/useCardGlowField";
import { CompactNavigation, type AppPage } from "./CompactNavigation";
import { TitleBar } from "./TitleBar";

export function AppShell({
  page,
  onPageChange,
  pageDirection,
  animatePage,
  children,
}: PropsWithChildren<{
  page: AppPage;
  onPageChange: (page: AppPage) => void;
  pageDirection: "forward" | "backward";
  animatePage: boolean;
}>) {
  useCardGlowField();

  return (
    <div className="app-shell">
      <TitleBar />
      <CompactNavigation page={page} onPageChange={onPageChange} />
      <div className="page-viewport">
        <div
          key={page}
          className={`page-transition-layer${animatePage ? " is-entering" : ""}`}
          data-direction={pageDirection}
        >
          {children}
        </div>
      </div>
    </div>
  );
}
