export type DomRefs = {
  sidebarEl: HTMLElement;
  sbScrollEl: HTMLDivElement;
  backendStatusEl: HTMLElement;
  backendStatusTextEl: HTMLElement;
  tailscaleStatusEl: HTMLElement;
  tailscaleStatusTextEl: HTMLElement;
  updateStatusEl: HTMLElement;
  updateStatusTextEl: HTMLElement;
  messagesEl: HTMLElement;
  statusViewEl: HTMLElement;
  bufferNameEl: HTMLElement;
  bufferTopicEl: HTMLElement;
  bufferMemcountEl: HTMLElement;
  memberCountInlineEl: HTMLElement;
  bufferOptionsBtnEl: HTMLButtonElement;
  toggleMembersEl: HTMLButtonElement;
  shortcutsHelpBtnEl: HTMLButtonElement;
  mobileMenuEl: HTMLButtonElement;
  inputEl: HTMLInputElement;
  inputForm: HTMLFormElement;
  uploadInputEl: HTMLInputElement;
  uploadButtonEl: HTMLButtonElement;
  inputNickEl: HTMLElement;
  cmdPopEl: HTMLElement;
  emojiPopEl: HTMLElement;
  nickPopEl: HTMLElement;
  memberListEl: HTMLElement;
  memberCountEl: HTMLElement;
  memberPaneEl: HTMLElement;
};

function mustEl<T extends HTMLElement>(id: string): T {
  const node = document.getElementById(id);
  if (!node) throw new Error(`missing element #${id}`);
  return node as T;
}

export function captureDom(): DomRefs {
  return {
    sidebarEl: mustEl<HTMLElement>("sidebar"),
    sbScrollEl: mustEl<HTMLDivElement>("sb-scroll"),
    backendStatusEl: mustEl<HTMLElement>("backend-status"),
    backendStatusTextEl: mustEl<HTMLElement>("backend-status-text"),
    tailscaleStatusEl: mustEl<HTMLElement>("tailscale-status"),
    tailscaleStatusTextEl: mustEl<HTMLElement>("tailscale-status-text"),
    updateStatusEl: mustEl<HTMLElement>("update-status"),
    updateStatusTextEl: mustEl<HTMLElement>("update-status-text"),
    messagesEl: mustEl<HTMLElement>("messages"),
    statusViewEl: mustEl<HTMLElement>("status-view"),
    bufferNameEl: mustEl<HTMLElement>("buffer-name"),
    bufferTopicEl: mustEl<HTMLElement>("buffer-topic"),
    bufferMemcountEl: mustEl<HTMLElement>("buffer-memcount"),
    memberCountInlineEl: mustEl<HTMLElement>("member-count-inline"),
    bufferOptionsBtnEl: mustEl<HTMLButtonElement>("buffer-options-btn"),
    toggleMembersEl: mustEl<HTMLButtonElement>("toggle-members"),
    shortcutsHelpBtnEl: mustEl<HTMLButtonElement>("shortcuts-help-btn"),
    mobileMenuEl: mustEl<HTMLButtonElement>("mobile-menu"),
    inputEl: mustEl<HTMLInputElement>("input"),
    inputForm: mustEl<HTMLFormElement>("input-form"),
    uploadInputEl: mustEl<HTMLInputElement>("upload-input"),
    uploadButtonEl: mustEl<HTMLButtonElement>("upload-button"),
    inputNickEl: mustEl<HTMLElement>("input-nick"),
    cmdPopEl: mustEl<HTMLElement>("cmd-pop"),
    emojiPopEl: mustEl<HTMLElement>("emoji-pop"),
    nickPopEl: mustEl<HTMLElement>("nick-pop"),
    memberListEl: mustEl<HTMLElement>("member-list"),
    memberCountEl: mustEl<HTMLElement>("member-count"),
    memberPaneEl: mustEl<HTMLElement>("member-pane"),
  };
}
