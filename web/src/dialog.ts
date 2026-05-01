export type DialogHandle = {
  dialog: HTMLDialogElement;
  close: () => void;
};

export type DialogOptions = {
  className?: string;
  onClose?: () => void;
};

export function openDialog(opts: DialogOptions = {}): DialogHandle {
  const dialog = document.createElement("dialog");
  if (opts.className) dialog.className = opts.className;
  document.body.appendChild(dialog);

  dialog.addEventListener("close", () => {
    dialog.remove();
    opts.onClose?.();
  });
  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close();
  });

  return {
    dialog,
    close: () => dialog.close(),
  };
}
