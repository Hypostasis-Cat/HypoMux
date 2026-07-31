import type { ComponentPropsWithoutRef, ElementType, PropsWithChildren } from "react";

type GlassSurfaceProps<T extends ElementType = "section"> = PropsWithChildren<{
  as?: T;
  tone?: "primary" | "secondary";
  className?: string;
}> &
  Omit<ComponentPropsWithoutRef<T>, "as" | "className" | "children">;

export function GlassSurface<T extends ElementType = "section">({
  as,
  tone = "primary",
  className = "",
  children,
  ...props
}: GlassSurfaceProps<T>) {
  const Component = as ?? "section";
  return (
    <Component className={`glass-surface ${className}`.trim()} data-tone={tone} {...props}>
      {children}
    </Component>
  );
}
