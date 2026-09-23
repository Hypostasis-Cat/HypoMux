type Point = { x: number; y: number };
export type WindowDragHost = {
  supportsWindowDrag(): boolean;
  windowDragState(): Promise<Point & { maximised: boolean; fullscreen: boolean }>;
  restoreWindowForDrag(): Promise<{ width: number; height: number }>;
  moveWindow(x: number, y: number): Promise<void>;
};

type Gesture = {
  pointer: number;
  start: Point;
  latest: Point;
  offsetY: number;
  fractionX: number;
  origin?: Point;
  maximised: boolean;
  moved: boolean;
  moving: boolean;
  ended: boolean;
};

// Wails' built-in drag listener handles mouse events only. Keep that native
// path intact; touch/pen use pointer capture and serialised window positions.
export function attachWindowTouchDrag(element: HTMLElement, host: WindowDragHost) {
  let gesture: Gesture | undefined;
  let frame: number | undefined;
  let disposed = false;
  const interactive = ".titlebar-actions, button, a, input, select, textarea, [role='button'], [contenteditable='true'], [data-no-window-drag]";
  const release = (g: Gesture) => {
    if (element.hasPointerCapture(g.pointer)) element.releasePointerCapture(g.pointer);
  };
  const abort = () => {
    const g = gesture;
    gesture = undefined;
    if (frame !== undefined) cancelAnimationFrame(frame);
    frame = undefined;
    element.removeAttribute("data-touch-window-drag");
    if (g) release(g);
  };
  const schedule = () => {
    if (frame === undefined && !disposed) frame = requestAnimationFrame(() => { frame = undefined; void move(); });
  };
  const move = async () => {
    const g = gesture;
    if (!g?.origin || g.moving) return;
    if (!g.moved && Math.hypot(g.latest.x - g.start.x, g.latest.y - g.start.y) < 6) {
      if (g.ended) abort();
      return;
    }
    g.moved = true;
    g.moving = true;
    try {
      if (g.maximised) {
        const size = await host.restoreWindowForDrag();
        if (gesture !== g) return;
        // Preserve the finger's relative horizontal grip when restoring.
        g.origin = { x: g.start.x - size.width * g.fractionX, y: g.start.y - g.offsetY };
        g.maximised = false;
      }
      const point = { ...g.latest };
      await host.moveWindow(g.origin.x + point.x - g.start.x, g.origin.y + point.y - g.start.y);
      if (gesture !== g) return;
      g.moving = false;
      if (point.x !== g.latest.x || point.y !== g.latest.y) schedule();
      else if (g.ended) abort();
    } catch {
      if (gesture === g) abort();
    }
  };
  const down = (event: PointerEvent) => {
    if (gesture || !event.isPrimary || event.button !== 0 || !["touch", "pen"].includes(event.pointerType) || !host.supportsWindowDrag()) return;
    if (!(event.target instanceof Element) || event.target.closest(interactive)) return;
    // Suppress compatibility mouse events so one gesture cannot start two drags.
    event.preventDefault();
    const g: Gesture = {
      pointer: event.pointerId, start: { x: event.screenX, y: event.screenY }, latest: { x: event.screenX, y: event.screenY },
      offsetY: event.clientY, fractionX: Math.max(0, Math.min(1, event.clientX / Math.max(1, window.innerWidth))),
      maximised: false, moved: false, moving: false, ended: false,
    };
    gesture = g;
    // WebView2 may still emit compatibility mouse events for a touch gesture.
    // Keep Wails' mouse drag listener inactive until this gesture is finished.
    element.setAttribute("data-touch-window-drag", "");
    try { element.setPointerCapture(event.pointerId); } catch { abort(); return; }
    void host.windowDragState().then(state => {
      if (gesture !== g) return;
      if (state.fullscreen) { abort(); return; }
      g.origin = { x: state.x, y: state.y };
      g.maximised = state.maximised;
      schedule();
    }).catch(() => { if (gesture === g) abort(); });
  };
  const update = (event: PointerEvent) => {
    const g = gesture;
    if (!g || g.pointer !== event.pointerId || g.ended) return;
    event.preventDefault();
    g.latest = { x: event.screenX, y: event.screenY };
    schedule();
  };
  const up = (event: PointerEvent) => {
    const g = gesture;
    if (!g || g.pointer !== event.pointerId) return;
    update(event);
    g.ended = true;
    release(g);
    // Keep the final position queued, including when the initial query is slow.
    schedule();
  };
  const cancel = (event: PointerEvent) => { if (gesture?.pointer === event.pointerId) abort(); };
  const lost = (event: PointerEvent) => { if (!gesture?.ended) cancel(event); };
  element.addEventListener("pointerdown", down);
  element.addEventListener("pointermove", update);
  element.addEventListener("pointerup", up);
  element.addEventListener("pointercancel", cancel);
  element.addEventListener("lostpointercapture", lost);
  window.addEventListener("blur", abort);
  return () => {
    disposed = true;
    abort();
    element.removeEventListener("pointerdown", down);
    element.removeEventListener("pointermove", update);
    element.removeEventListener("pointerup", up);
    element.removeEventListener("pointercancel", cancel);
    element.removeEventListener("lostpointercapture", lost);
    window.removeEventListener("blur", abort);
  };
}
