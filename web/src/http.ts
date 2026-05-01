export async function jsonFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) throw new Error((await res.text()) || res.statusText);
  return (await res.json()) as T;
}

export function sendJSON<T>(url: string, method: "POST" | "PATCH" | "PUT" | "DELETE", body: unknown): Promise<T> {
  return jsonFetch<T>(url, {
    method,
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}
