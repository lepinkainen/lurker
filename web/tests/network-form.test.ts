import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Network } from "../src/app-state";
import { openNetworkForm } from "../src/network-form";

type SentRequest = { url: string; method: string; body: Record<string, unknown> };

function stubFetch(): SentRequest[] {
  const sent: SentRequest[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = (init?.method ?? "GET").toUpperCase();
      // Edit path loads connect-commands on open.
      if (url.includes("/connect-commands")) {
        return Promise.resolve(Response.json({ commands: [] }));
      }
      const body = init?.body ? JSON.parse(String(init.body)) : {};
      sent.push({ url, method, body });
      return Promise.resolve(Response.json({ id: body.id ?? "net-1", ...body }));
    }),
  );
  return sent;
}

function submit(): Promise<void> {
  const form = document.querySelector("form.nf-form") as HTMLFormElement;
  const submitBtn = form.querySelector("button[type=submit]") as HTMLButtonElement;
  form.requestSubmit(submitBtn);
  // Let the async submit handler's fetch + close resolve. Throw (not expect)
  // so the assertion stays inside the it() body, not this helper.
  return vi.waitFor(() => {
    if (document.querySelector("form.nf-form")) throw new Error("form still open");
  });
}

function setValue(id: string, value: string) {
  const el = document.getElementById(id) as HTMLInputElement;
  el.value = value;
}

const existing: Network = {
  id: "net-1",
  name: "Libera",
  host: "irc.libera.chat",
  port: 6697,
  tls: true,
  nick: "tester",
  realname: "Test",
};

describe("network form SASL handling", () => {
  beforeEach(() => {
    document.body.replaceChildren();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    document.body.replaceChildren();
  });

  it("does not send SASL fields on edit when the user leaves them blank", async () => {
    const sent = stubFetch();
    openNetworkForm(existing);
    // Wait for the connect-commands fetch on open to settle.
    await vi.waitFor(() => expect(fetch).toHaveBeenCalled());

    // User edits nothing SASL-related.
    await submit();

    const patch = sent.find((r) => r.method === "PATCH");
    expect(patch).toBeDefined();
    expect(patch?.body).not.toHaveProperty("sasl_user");
    expect(patch?.body).not.toHaveProperty("sasl_pass");
  });

  it("includes a newly typed password on edit", async () => {
    const sent = stubFetch();
    openNetworkForm(existing);
    await vi.waitFor(() => expect(fetch).toHaveBeenCalled());

    setValue("nf-sasl-pass", "hunter2");
    setValue("nf-sasl-user", "tester");
    await submit();

    const patch = sent.find((r) => r.method === "PATCH");
    expect(patch?.body.sasl_pass).toBe("hunter2");
    expect(patch?.body.sasl_user).toBe("tester");
  });

  it("always sends SASL fields on create", async () => {
    const sent = stubFetch();
    openNetworkForm();

    setValue("nf-name", "New");
    setValue("nf-host", "irc.example.org");
    setValue("nf-nick", "me");
    await submit();

    const post = sent.find((r) => r.method === "POST");
    expect(post).toBeDefined();
    expect(post?.body).toHaveProperty("sasl_user", "");
    expect(post?.body).toHaveProperty("sasl_pass", "");
  });
});
