export type DomRefs = {
  sbScrollEl: HTMLDivElement;
  backendStatusEl: HTMLElement;
  backendStatusTextEl: HTMLElement;
  tailscaleStatusEl: HTMLElement;
  tailscaleStatusTextEl: HTMLElement;
  messagesEl: HTMLElement;
  statusViewEl: HTMLElement;
  bufferNameEl: HTMLElement;
  bufferTopicEl: HTMLElement;
  bufferMemcountEl: HTMLElement;
  memberCountInlineEl: HTMLElement;
  toggleMembersEl: HTMLButtonElement;
  mobileMenuEl: HTMLButtonElement;
  inputEl: HTMLInputElement;
  inputForm: HTMLFormElement;
  inputNickEl: HTMLElement;
  cmdPopEl: HTMLElement;
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
    sbScrollEl: mustEl<HTMLDivElement>("sb-scroll"),
    backendStatusEl: mustEl<HTMLElement>("backend-status"),
    backendStatusTextEl: mustEl<HTMLElement>("backend-status-text"),
    tailscaleStatusEl: mustEl<HTMLElement>("tailscale-status"),
    tailscaleStatusTextEl: mustEl<HTMLElement>("tailscale-status-text"),
    messagesEl: mustEl<HTMLElement>("messages"),
    statusViewEl: mustEl<HTMLElement>("status-view"),
    bufferNameEl: mustEl<HTMLElement>("buffer-name"),
    bufferTopicEl: mustEl<HTMLElement>("buffer-topic"),
    bufferMemcountEl: mustEl<HTMLElement>("buffer-memcount"),
    memberCountInlineEl: mustEl<HTMLElement>("member-count-inline"),
    toggleMembersEl: mustEl<HTMLButtonElement>("toggle-members"),
    mobileMenuEl: mustEl<HTMLButtonElement>("mobile-menu"),
    inputEl: mustEl<HTMLInputElement>("input"),
    inputForm: mustEl<HTMLFormElement>("input-form"),
    inputNickEl: mustEl<HTMLElement>("input-nick"),
    cmdPopEl: mustEl<HTMLElement>("cmd-pop"),
    memberListEl: mustEl<HTMLElement>("member-list"),
    memberCountEl: mustEl<HTMLElement>("member-count"),
    memberPaneEl: mustEl<HTMLElement>("member-pane"),
  };
}
