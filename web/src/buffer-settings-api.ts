import type { Buffer } from "./app-state";

export type BufferSettingsPatch = Partial<
  Pick<Buffer, "show_embeds" | "show_presence_events" | "collapse_presence_events" | "pinned">
>;

export async function patchBufferSettings(
  id: number,
  patch: BufferSettingsPatch,
): Promise<BufferSettingsPatch & { id: number }> {
  const res = await fetch(`/api/buffers/${id}/settings`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) throw new Error(await res.text());
  return (await res.json()) as BufferSettingsPatch & { id: number };
}
