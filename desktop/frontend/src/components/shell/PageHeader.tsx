import type { ReactNode, Ref } from "react";

type PageHeaderProps = {
  title: ReactNode;
  kicker?: ReactNode;
  description?: ReactNode;
  /** Rendered above the copy, e.g. a back button in nested tool views. */
  leading?: ReactNode;
  /** Right-aligned page actions; pages keep their own action wrapper. */
  actions?: ReactNode;
  /** Changing the key replays the copy's entrance when the page swaps views. */
  copyKey?: string;
  copyClassName?: string;
  titleRef?: Ref<HTMLHeadingElement>;
  /** Lets nested views move focus to the title after navigation. */
  titleFocusable?: boolean;
};

/** The one page heading used by every top-level page and tool view. */
export function PageHeader({ title, kicker, description, leading, actions, copyKey, copyClassName, titleRef, titleFocusable }: PageHeaderProps) {
  return (
    <header className="page-heading">
      <div key={copyKey} className={copyClassName ? `page-heading-copy ${copyClassName}` : "page-heading-copy"}>
        {leading}
        {kicker && <span className="section-kicker">{kicker}</span>}
        <h1 ref={titleRef} tabIndex={titleFocusable ? -1 : undefined}>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {actions}
    </header>
  );
}
