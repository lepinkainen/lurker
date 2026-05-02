import { isMobileViewport, setSidebarDrawer } from "./ui-shell";

const EDGE_OPEN_PX = 24;
const MIN_SWIPE_PX = 60;
const MAX_VERTICAL_DRIFT_PX = 80;
const HORIZONTAL_DOMINANCE = 1.5;

type SwipeDirection = "left" | "right";

type GestureStart = {
  id: number;
  x: number;
  y: number;
  mode: "open-sidebar" | "close-sidebar";
};

export type SwipeDecision = {
  direction: SwipeDirection;
  horizontal: number;
  vertical: number;
};

export function classifyHorizontalSwipe(
  startX: number,
  startY: number,
  endX: number,
  endY: number,
): SwipeDecision | null {
  const dx = endX - startX;
  const dy = endY - startY;
  const horizontal = Math.abs(dx);
  const vertical = Math.abs(dy);
  if (horizontal < MIN_SWIPE_PX) return null;
  if (vertical > MAX_VERTICAL_DRIFT_PX) return null;
  if (horizontal < vertical * HORIZONTAL_DOMINANCE) return null;
  return { direction: dx > 0 ? "right" : "left", horizontal, vertical };
}

export function initTouchGestures(opts: { sidebarEl: HTMLElement; root?: Document }): () => void {
  const root = opts.root ?? document;
  let start: GestureStart | null = null;

  const sidebarOpen = () => document.body.dataset.sidebarOpen === "true";

  const onPointerDown = (event: PointerEvent) => {
    if (!isMobileViewport() || event.pointerType === "mouse") return;
    const target = event.target as HTMLElement | null;
    if (!target) return;

    if (sidebarOpen() && opts.sidebarEl.contains(target)) {
      start = { id: event.pointerId, x: event.clientX, y: event.clientY, mode: "close-sidebar" };
      return;
    }

    if (!sidebarOpen() && event.clientX <= EDGE_OPEN_PX) {
      start = { id: event.pointerId, x: event.clientX, y: event.clientY, mode: "open-sidebar" };
    }
  };

  const finish = (event: PointerEvent) => {
    if (!start || event.pointerId !== start.id) return;
    const current = start;
    start = null;

    if (!isMobileViewport()) return;
    const swipe = classifyHorizontalSwipe(current.x, current.y, event.clientX, event.clientY);
    if (!swipe) return;

    if (current.mode === "open-sidebar" && swipe.direction === "right") setSidebarDrawer(true);
    if (current.mode === "close-sidebar" && swipe.direction === "left") setSidebarDrawer(false);
  };

  const cancel = (event: PointerEvent) => {
    if (start && event.pointerId === start.id) start = null;
  };

  root.addEventListener("pointerdown", onPointerDown);
  root.addEventListener("pointerup", finish);
  root.addEventListener("pointercancel", cancel);

  return () => {
    root.removeEventListener("pointerdown", onPointerDown);
    root.removeEventListener("pointerup", finish);
    root.removeEventListener("pointercancel", cancel);
  };
}
