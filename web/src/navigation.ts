import { state } from "./app-state";
import { bufferFromHash } from "./router";

export type SetActive = (id: number, opts?: { skipHash?: boolean; replaceHash?: boolean }) => void;

export function onHashChange(setActive: SetActive) {
  const buffer = bufferFromHash(location.hash);
  if (buffer && buffer.id !== state.activeId) setActive(buffer.id, { skipHash: true });
}
