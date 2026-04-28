import { type Network, state } from "./app-state";

type FormResult = Network & { nick: string };

function field(label: string, input: HTMLElement): HTMLDivElement {
  const row = document.createElement("div");
  row.className = "nf-field";
  const lbl = document.createElement("label");
  lbl.className = "nf-label";
  lbl.textContent = label;
  row.append(lbl, input);
  return row;
}

function textInput(id: string, value = "", placeholder = ""): HTMLInputElement {
  const el = document.createElement("input");
  el.type = "text";
  el.id = id;
  el.className = "nf-input";
  el.value = value;
  el.placeholder = placeholder;
  return el;
}

function passwordInput(id: string, placeholder = ""): HTMLInputElement {
  const el = document.createElement("input");
  el.type = "password";
  el.id = id;
  el.className = "nf-input";
  el.placeholder = placeholder;
  el.autocomplete = "new-password";
  return el;
}

function numberInput(id: string, value: number, min: number, max: number): HTMLInputElement {
  const el = document.createElement("input");
  el.type = "number";
  el.id = id;
  el.className = "nf-input nf-input-sm";
  el.value = String(value);
  el.min = String(min);
  el.max = String(max);
  return el;
}

export function openNetworkForm(existing?: Network, onDone?: (n: FormResult) => void): void {
  const isEdit = existing != null;

  const dialog = document.createElement("dialog");
  dialog.className = "nf-dialog";

  const form = document.createElement("form");
  form.method = "dialog";
  form.className = "nf-form";
  form.noValidate = true;

  const title = document.createElement("h2");
  title.className = "nf-title";
  title.textContent = isEdit ? "Edit network" : "Add network";

  const nameEl = textInput("nf-name", existing?.name ?? "");
  nameEl.required = true;

  const hostEl = textInput("nf-host", existing?.host ?? "");
  hostEl.required = true;

  const portEl = numberInput("nf-port", existing?.port ?? 6697, 1, 65535);

  const tlsEl = document.createElement("input");
  tlsEl.type = "checkbox";
  tlsEl.id = "nf-tls";
  tlsEl.className = "nf-checkbox";
  tlsEl.checked = existing?.tls ?? true;
  tlsEl.addEventListener("change", () => {
    const defaultPort = tlsEl.checked ? 6697 : 6669;
    if (portEl.value === "6697" || portEl.value === "6669") portEl.value = String(defaultPort);
  });

  const nickEl = textInput("nf-nick", existing?.nick ?? "");
  nickEl.required = true;

  const realnameEl = textInput("nf-realname", existing?.realname ?? "");

  const disabledEl = document.createElement("input");
  disabledEl.type = "checkbox";
  disabledEl.id = "nf-disabled";
  disabledEl.className = "nf-checkbox";
  disabledEl.checked = existing?.disabled ?? false;
  const disabledLabel = document.createElement("label");
  disabledLabel.className = "nf-checkbox-label";
  disabledLabel.htmlFor = "nf-disabled";
  disabledLabel.textContent = "Disabled";
  const disabledWrap = document.createElement("div");
  disabledWrap.className = "nf-checkbox-wrap nf-disabled-wrap";
  disabledWrap.append(disabledEl, disabledLabel);

  const saslUserEl = textInput("nf-sasl-user", "");
  saslUserEl.autocomplete = "username";

  const saslPassEl = passwordInput("nf-sasl-pass", isEdit ? "leave blank to keep existing" : "");

  const portRow = document.createElement("div");
  portRow.className = "nf-row";
  const portField = field("Port", portEl);
  const tlsLabel = document.createElement("label");
  tlsLabel.className = "nf-checkbox-label";
  tlsLabel.htmlFor = "nf-tls";
  const tlsWrap = document.createElement("div");
  tlsWrap.className = "nf-checkbox-wrap";
  tlsWrap.append(tlsEl, tlsLabel);
  tlsLabel.textContent = "TLS";
  portRow.append(portField, tlsWrap);

  const saslSection = document.createElement("details");
  saslSection.className = "nf-section";
  const saslSummary = document.createElement("summary");
  saslSummary.className = "nf-section-title";
  saslSummary.textContent = "SASL authentication (optional)";
  saslSection.append(saslSummary, field("Username", saslUserEl), field("Password", saslPassEl));

  const errEl = document.createElement("p");
  errEl.className = "nf-error";
  errEl.hidden = true;

  const actions = document.createElement("div");
  actions.className = "nf-actions";

  const cancelBtn = document.createElement("button");
  cancelBtn.type = "button";
  cancelBtn.className = "nf-btn";
  cancelBtn.textContent = "Cancel";
  cancelBtn.addEventListener("click", () => dialog.close());

  const submitBtn = document.createElement("button");
  submitBtn.type = "submit";
  submitBtn.className = "nf-btn nf-btn-primary";
  submitBtn.textContent = isEdit ? "Save" : "Add network";

  actions.append(cancelBtn, submitBtn);

  form.append(
    title,
    field("Name", nameEl),
    field("Host", hostEl),
    portRow,
    field("Nick", nickEl),
    field("Real name", realnameEl),
    ...(isEdit ? [disabledWrap] : []),
    saslSection,
    errEl,
    actions,
  );

  dialog.appendChild(form);
  document.body.appendChild(dialog);

  dialog.addEventListener("close", () => {
    dialog.remove();
  });

  dialog.addEventListener("click", (e) => {
    if (e.target === dialog) dialog.close();
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    errEl.hidden = true;

    const name = nameEl.value.trim();
    const host = hostEl.value.trim();
    const port = parseInt(portEl.value, 10);
    const nick = nickEl.value.trim();
    const realname = realnameEl.value.trim();
    const saslUser = saslUserEl.value.trim();
    const saslPass = saslPassEl.value;

    if (!name || !host || !port || !nick) {
      errEl.textContent = "Name, host, port, and nick are required.";
      errEl.hidden = false;
      return;
    }

    const body: Record<string, unknown> = {
      name,
      host,
      port,
      tls: tlsEl.checked,
      nick,
      realname,
      sasl_user: saslUser,
      sasl_pass: saslPass,
    };
    if (isEdit) body.disabled = disabledEl.checked;

    submitBtn.disabled = true;
    submitBtn.textContent = isEdit ? "Saving…" : "Adding…";

    try {
      const url = existing ? `/api/networks/${existing.id}` : "/api/networks";
      const method = isEdit ? "PATCH" : "POST";
      const res = await fetch(url, {
        method,
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(text || res.statusText);
      }
      const data = (await res.json()) as FormResult;
      state.networks.set(data.id, data);
      dialog.close();
      onDone?.(data);
    } catch (err) {
      errEl.textContent = err instanceof Error ? err.message : "Request failed";
      errEl.hidden = false;
      submitBtn.disabled = false;
      submitBtn.textContent = isEdit ? "Save" : "Add network";
    }
  });

  dialog.showModal();
  nameEl.focus();
}
