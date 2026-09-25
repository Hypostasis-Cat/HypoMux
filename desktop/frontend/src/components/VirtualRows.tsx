import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode, type RefObject } from "react";

export type VirtualRow = { key: string; render: () => ReactNode; estimate?: number };

/** Window large lists; measured heights support wrapped labels and expanded groups. */
export function VirtualRows({ rows, scrollRef }: { rows: VirtualRow[]; scrollRef: RefObject<HTMLDivElement> }) {
  const [viewport, setViewport] = useState({ top: 0, height: 600 });
  const [heights, setHeights] = useState<Map<string, number>>(() => new Map());
  const host = useRef<HTMLDivElement>(null);
  const virtual = rows.length > 80;
  useEffect(() => {
    const keys = new Set(rows.map(row => row.key));
    setHeights(previous => [...previous.keys()].some(key => !keys.has(key))
      ? new Map([...previous].filter(([key]) => keys.has(key))) : previous);
  }, [rows]);
  const offsets = useMemo(() => {
    const values = [0];
    rows.forEach(row => values.push(values[values.length - 1] + (heights.get(row.key) ?? row.estimate ?? 72)));
    return values;
  }, [rows, heights]);
  const total = offsets[offsets.length - 1];
  const top = Math.min(viewport.top, Math.max(0, total - viewport.height));
  let start = 0, end = rows.length;
  if (virtual) {
    while (start < rows.length && offsets[start + 1] < top - 300) start++;
    end = start;
    while (end < rows.length && offsets[end] < top + viewport.height + 300) end++;
  }

  useEffect(() => {
    const scroll = scrollRef.current;
    if (!scroll || !virtual) return;
    let frame = 0;
    const read = () => {
      frame = 0;
      setViewport({ top: scroll.scrollTop, height: scroll.clientHeight || 600 });
    };
    const schedule = () => { if (!frame) frame = requestAnimationFrame(read); };
    read();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(scroll);
    scroll.addEventListener("scroll", schedule, { passive: true });
    return () => { cancelAnimationFrame(frame); observer?.disconnect(); scroll.removeEventListener("scroll", schedule); };
  }, [scrollRef, virtual]);

  useLayoutEffect(() => {
    if (!virtual || !host.current || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(entries => {
      setHeights(previous => {
        const next = new Map(previous);
        let changed = false;
        entries.forEach(entry => {
          const key = (entry.target as HTMLElement).dataset.rowKey!;
          const height = entry.borderBoxSize?.[0]?.blockSize ?? entry.target.getBoundingClientRect().height;
          if (height > 0 && Math.abs((next.get(key) ?? 0) - height) > .5) { next.set(key, height); changed = true; }
        });
        return changed ? next : previous;
      });
    });
    host.current.querySelectorAll<HTMLElement>("[data-row-key]").forEach(element => observer.observe(element));
    return () => observer.disconnect();
  }, [start, end, virtual, rows]);

  if (!virtual) return <div>{rows.map(row => <div key={row.key}>{row.render()}</div>)}</div>;
  return <div ref={host}>
    <div aria-hidden="true" style={{ height: offsets[start] }} />
    {rows.slice(start, end).map(row => <div key={row.key} data-row-key={row.key}>{row.render()}</div>)}
    <div aria-hidden="true" style={{ height: total - offsets[end] }} />
  </div>;
}
