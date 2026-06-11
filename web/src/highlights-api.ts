import { jsonFetch, sendJSON } from "./http";

type HighlightsResponse = { patterns?: string[] };

export async function getHighlights(): Promise<string[]> {
  const data = await jsonFetch<HighlightsResponse>("/api/settings/highlights");
  return data.patterns || [];
}

export async function putHighlights(patterns: string[]): Promise<string[]> {
  const data = await sendJSON<HighlightsResponse>("/api/settings/highlights", "PUT", { patterns });
  return data.patterns || [];
}
