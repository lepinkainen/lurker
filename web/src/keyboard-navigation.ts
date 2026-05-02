import { state } from "./app-state";
import { getVisibleSidebarBufferIds } from "./buffers";

function findMatchingBufferId(
  startId: string | null,
  ids: string[],
  predicate: (id: string) => boolean,
  direction: 1 | -1,
): string | null {
  if (ids.length === 0) return null;
  const fallback = direction === 1 ? -1 : 0;
  const startIndex = startId === null ? fallback : ids.indexOf(startId);
  const seed = Math.max(startIndex, fallback);
  for (let step = 1; step <= ids.length; step += 1) {
    const index = (seed + direction * step + ids.length) % ids.length;
    // biome-ignore lint/style/noNonNullAssertion: modulo index is always in bounds after length check
    const id = ids[index]!;
    if (id !== startId && predicate(id)) return id;
  }
  return null;
}

function isUnreadMessageBuffer(id: string): boolean {
  const buffer = state.buffers.get(id);
  return buffer !== undefined && buffer.kind === "channel" && buffer.unread > 0;
}

function activeChannelIds(): string[] {
  return getVisibleSidebarBufferIds().filter((id) => state.buffers.get(id)?.kind === "channel");
}

export function navigateBuffer(setActive: (id: string) => void, previous = false) {
  const next = findMatchingBufferId(state.activeId, getVisibleSidebarBufferIds(), () => true, previous ? -1 : 1);
  if (next !== null) setActive(next);
}

export function navigateUnread(setActive: (id: string) => void, previous = false) {
  const next = findMatchingBufferId(state.activeId, activeChannelIds(), isUnreadMessageBuffer, previous ? -1 : 1);
  if (next !== null) setActive(next);
}

export function navigateMention(setActive: (id: string) => void) {
  const next = findMatchingBufferId(
    state.activeId,
    activeChannelIds(),
    (id) => (state.buffers.get(id)?.mentions || 0) > 0,
    1,
  );
  if (next !== null) setActive(next);
}

export function navigateFirstUnread(setActive: (id: string) => void) {
  const firstUnread = activeChannelIds().find(isUnreadMessageBuffer);
  if (firstUnread !== undefined && firstUnread !== state.activeId) setActive(firstUnread);
}

export function focusInput(inputEl: HTMLInputElement) {
  if (inputEl.disabled) return;
  inputEl.focus();
}
