export function buildDialog(className: string, overlay: string) {
  const dialog = document.createElement("dialog");
  dialog.className = className;
  dialog.dataset.overlay = overlay;
  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close();
  });
  dialog.addEventListener("cancel", (e) => {
    e.preventDefault();
    dialog.close();
  });
  return dialog;
}

export function canOpenModal() {
  const open = document.querySelector("dialog[open]");
  return !open;
}
