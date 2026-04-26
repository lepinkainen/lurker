export function isMobileViewport(): boolean {
  return window.matchMedia("(max-width: 640px)").matches;
}

export function setSidebarDrawer(open: boolean) {
  if (open) document.body.dataset.sidebarOpen = "true";
  else delete document.body.dataset.sidebarOpen;
}

export function setMembersDrawer(open: boolean) {
  if (open) document.body.dataset.membersOpen = "true";
  else delete document.body.dataset.membersOpen;
}

export function closeAllDrawers() {
  setSidebarDrawer(false);
  setMembersDrawer(false);
}

export function onBackdropClick(e: MouseEvent) {
  if (!isMobileViewport()) return;
  const sidebarOpen = document.body.dataset.sidebarOpen === "true";
  const membersOpen = document.body.dataset.membersOpen === "true";
  if (!sidebarOpen && !membersOpen) return;
  const target = e.target as HTMLElement | null;
  if (!target) return;
  if (sidebarOpen && !target.closest("#sidebar") && !target.closest("#mobile-menu")) {
    setSidebarDrawer(false);
  }
  if (membersOpen && !target.closest("#member-pane") && !target.closest("#toggle-members")) {
    setMembersDrawer(false);
  }
}
