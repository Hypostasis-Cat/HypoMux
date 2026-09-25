import { useCallback, useEffect, useState, type PropsWithChildren, type ReactNode } from "react";
import { PageActivity } from "./PageActivity";
import { AIAssistant } from "../ai/AIAssistant";
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
  renderPage,
  children,
}: PropsWithChildren<{
  page: AppPage;
  onPageChange: (page: AppPage) => void;
  pageDirection: "forward" | "backward";
  animatePage: boolean;
  persistentPage?: AppPage;
  persistentChildren?: ReactNode;
  renderPage?: (page: AppPage) => ReactNode;
}>) {
  useCardGlowField();
  const { locale } = useI18n();
  const [assistantOpen, setAssistantOpen] = useState(false);
  const [visited, setVisited] = useState<AppPage[]>([page]);
  const pages = visited.includes(page) ? visited : [...visited, page];
  useEffect(() => { setVisited(current => current.includes(page) ? current : [...current, page]); }, [page]);
  const openWorkspace = useCallback(() => { setAssistantOpen(false); onPageChange("assistant"); }, [onPageChange]);

  return (
    <div className="app-shell">
      <a className="skip-to-content" href="#page-content" onClick={(event) => {
        event.preventDefault();
        document.getElementById(page === "assistant" ? "ai-workspace" : "page-content")?.focus();
      }}>{locale === "en" ? "Skip to content" : "跳转到页面内容"}</a>
      <TitleBar />
      <CompactNavigation page={page} onPageChange={onPageChange} />
      <div id="page-content" className="page-viewport" hidden={page === "assistant"} tabIndex={-1}>
        {persistentPage && persistentChildren ? (
          <div
            className={`page-transition-layer${page === persistentPage && animatePage ? " is-entering" : ""}`}
            data-direction={pageDirection}
            hidden={page !== persistentPage}
          >
            <PageActivity.Provider value={page === persistentPage}>{persistentChildren}</PageActivity.Provider>
          </div>
        ) : null}
        {renderPage ? pages.filter(item => item !== persistentPage && item !== "assistant").map(item => (
          <div key={item} className={`page-transition-layer${page === item && animatePage ? " is-entering" : ""}`} data-direction={pageDirection} hidden={page !== item}>
            <PageActivity.Provider value={page === item}>{renderPage(item)}</PageActivity.Provider>
          </div>
        )) : page !== persistentPage && page !== "assistant" ? (
          <div
            key={page}
            className={`page-transition-layer${animatePage ? " is-entering" : ""}`}
            data-direction={pageDirection}
          >
            {children}
          </div>
        ) : null}
      </div>
      <AIAssistant page={page} open={assistantOpen || page === "assistant"} workspace={page === "assistant"} onOpenChange={setAssistantOpen} onOpenWorkspace={openWorkspace} />
    </div>
  );
}
