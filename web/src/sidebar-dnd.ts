import { state } from "./app-state";

// Sentinel drop target for "after the last row". Not an entity id, so it can
// share `over` with real row targets. Network and pinned drags keep separate
// drag state, so one sentinel value serves both.
export const DROP_END = "__drop_end__";

// `{ id, over }` — id is the entity being dragged, over is the current drop
// target (an entity id or DROP_END).
export type DragState = { id: string | null; over: string | null };

export type ReorderDragDeps = {
  render: () => void;
  // toId is the entity to insert before, or null to append at the end.
  reorder: (fromId: string, toId: string | null) => void;
};

// Both sidebar drags use strict insert-before semantics: the accent bar renders
// above the hovered element and the dragged entity lands in that slot. The final
// position is reached through the end-of-list zone (see endDropZone).
export function attachReorderDragHandlers(el: HTMLElement, id: string, drag: DragState, deps: ReorderDragDeps): void {
  el.draggable = true;
  el.addEventListener("dragstart", (e) => {
    drag.id = id;
    try {
      e.dataTransfer?.setData("text/plain", id);
      if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
    } catch {
      // Some browsers restrict DataTransfer mutation; drag state still works without it.
    }
    el.classList.add("dragging");
  });
  el.addEventListener("dragend", () => {
    clearDrag(drag);
    deps.render();
  });
  el.addEventListener("dragover", (e) => {
    if (drag.id === null || drag.id === id) return;
    e.preventDefault();
    if (drag.over !== id) {
      drag.over = id;
      deps.render();
    }
  });
  el.addEventListener("dragleave", () => {
    if (drag.over === id) drag.over = null;
  });
  el.addEventListener("drop", (e) => {
    e.preventDefault();
    const fromId = drag.id;
    clearDrag(drag);
    if (fromId !== null && fromId !== id) deps.reorder(fromId, id);
    deps.render();
  });
}

// Thin strip below the last row of a draggable list. Row targets always insert
// *before* the hovered row, so without this zone the last slot is unreachable.
// Always laid out (not created on dragstart) because building it lazily would
// need a re-render inside the dragstart handler, which tears out the drag
// source node and aborts the drag.
export function endDropZone(modifierClass: string, drag: DragState, deps: ReorderDragDeps): HTMLElement {
  const zone = document.createElement("div");
  zone.className = ["sb-drop-end", modifierClass, drag.over === DROP_END && "dragover"].filter(Boolean).join(" ");
  zone.addEventListener("dragover", (e) => {
    if (drag.id === null) return;
    e.preventDefault();
    if (drag.over !== DROP_END) {
      drag.over = DROP_END;
      deps.render();
    }
  });
  zone.addEventListener("dragleave", () => {
    if (drag.over === DROP_END) drag.over = null;
  });
  zone.addEventListener("drop", (e) => {
    e.preventDefault();
    const fromId = drag.id;
    clearDrag(drag);
    if (fromId !== null) deps.reorder(fromId, null);
    deps.render();
  });
  return zone;
}

export function attachNetworkDragHandlers(sec: HTMLElement, networkId: string, deps: ReorderDragDeps): void {
  attachReorderDragHandlers(sec, networkId, state.drag, deps);
}

export function networkEndDropZone(deps: ReorderDragDeps): HTMLElement {
  return endDropZone("sb-net-end", state.drag, deps);
}

export function attachPinnedDragHandlers(row: HTMLElement, bufferId: string, deps: ReorderDragDeps): void {
  attachReorderDragHandlers(row, bufferId, state.pinDrag, deps);
}

export function pinnedEndDropZone(deps: ReorderDragDeps): HTMLElement {
  return endDropZone("sb-pin-end", state.pinDrag, deps);
}

function clearDrag(drag: DragState) {
  drag.id = null;
  drag.over = null;
}
