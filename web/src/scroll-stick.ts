export type ScrollStick = {
  isPinned: () => boolean;
  snap: () => void;
  watch: (scope: ParentNode) => void;
};

export function createScrollStick(el: HTMLElement, threshold = 8): ScrollStick {
  let pinned = true;
  el.addEventListener(
    "scroll",
    () => {
      pinned = el.scrollHeight - el.scrollTop - el.clientHeight <= threshold;
    },
    { passive: true },
  );
  const snap = () => {
    el.scrollTop = el.scrollHeight;
    pinned = true;
  };
  const watch = (scope: ParentNode) => {
    const imgs = scope.querySelectorAll<HTMLImageElement>("img:not([data-stick-watched])");
    for (const img of imgs) {
      if (img.complete) continue;
      img.dataset.stickWatched = "1";
      const settle = () => {
        if (pinned) snap();
      };
      img.addEventListener("load", settle, { once: true });
      img.addEventListener("error", settle, { once: true });
    }
  };
  return { isPinned: () => pinned, snap, watch };
}
