import type { Buffer } from "./app-state";

export type BufferSettingsPatch = Partial<
  Pick<Buffer, "show_embeds" | "show_presence_events" | "collapse_presence_events" | "pinned" | "archived">
>;

export async function patchBufferSettings(
  id: string,
  patch: BufferSettingsPatch,
): Promise<BufferSettingsPatch & { id: string }> {
  const res = await fetch(`/api/buffers/${id}/settings`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) throw new Error(await res.text());
  return (await res.json()) as BufferSettingsPatch & { id: string };
}
