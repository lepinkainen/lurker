import { type Network, state } from "./app-state";

export type NetworkDragDeps = {
  render: () => void;
  reorder: (fromId: number, toId: number) => void;
};

export function attachNetworkDragHandlers(sec: HTMLElement, network: Network, deps: NetworkDragDeps): void {
  sec.draggable = true;
  sec.addEventListener("dragstart", (e) => {
    state.drag.id = network.id;
    try {
      e.dataTransfer?.setData("text/plain", String(network.id));
      if (e.dataTransfer) e.dataTransfer.effectAllowed = "move";
    } catch {
      // Some browsers restrict DataTransfer mutation; drag state still works without it.
    }
    sec.classList.add("dragging");
  });
  sec.addEventListener("dragend", () => {
    state.drag.id = null;
    state.drag.over = null;
    deps.render();
  });
  sec.addEventListener("dragover", (e) => {
    if (state.drag.id === null || state.drag.id === network.id) return;
    e.preventDefault();
    if (state.drag.over !== network.id) {
      state.drag.over = network.id;
      deps.render();
    }
  });
  sec.addEventListener("dragleave", () => {
    if (state.drag.over === network.id) state.drag.over = null;
  });
  sec.addEventListener("drop", (e) => {
    e.preventDefault();
    const fromId = state.drag.id;
    if (fromId !== null && fromId !== network.id) deps.reorder(fromId, network.id);
    state.drag.id = null;
    state.drag.over = null;
    deps.render();
  });
}
