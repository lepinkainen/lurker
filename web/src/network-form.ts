import { type Network, state } from "./app-state";
import { openDialog } from "./dialog";
import { sendJSON } from "./http";

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

function textareaInput(id: string, value = "", placeholder = ""): HTMLTextAreaElement {
  const el = document.createElement("textarea");
  el.id = id;
  el.className = "nf-input";
  el.rows = 5;
  el.value = value;
  el.placeholder = placeholder;
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
  const isEdit = existing !== undefined;

  const { dialog, close } = openDialog({ className: "nf-dialog" });

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

  const portEl = numberInput("nf-port", existing?.port ?? 6697, 1, 65_535);

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

  // The SASL username is not prefilled on edit because the API never ships SASL
  // credentials to clients (see networkDTO in api/state.go). Leaving it blank on
  // edit is safe: the backend treats a blank username as "keep existing".
  const saslUserEl = textInput("nf-sasl-user", "");
  saslUserEl.autocomplete = "username";

  const saslPassEl = passwordInput("nf-sasl-pass", isEdit ? "leave blank to keep existing" : "");

  const connectCommandsEl = textareaInput(
    "nf-connect-commands",
    "",
    "PRIVMSG NickServ :IDENTIFY hunter2\nMODE mynick +x",
  );
  const connectCommandsHelp = document.createElement("p");
  connectCommandsHelp.className = "nf-help";
  connectCommandsHelp.textContent =
    "Raw IRC commands sent after connect/registration, before autojoin. One command per line. May contain secrets.";

  if (isEdit) {
    fetch(`/api/networks/${existing.id}/connect-commands`)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json() as Promise<{ commands?: string[] }>;
      })
      .then((data) => {
        connectCommandsEl.value = (data.commands ?? []).join("\n");
      })
      .catch(() => {
        connectCommandsHelp.textContent =
          "Could not load connect commands. Saving will replace them with the textarea contents.";
      });
  }

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

  const connectCommandsSection = document.createElement("details");
  connectCommandsSection.className = "nf-section";
  const connectCommandsSummary = document.createElement("summary");
  connectCommandsSummary.className = "nf-section-title";
  connectCommandsSummary.textContent = "Connect commands (optional)";
  connectCommandsSection.append(
    connectCommandsSummary,
    field("Connect commands", connectCommandsEl),
    connectCommandsHelp,
  );

  const errEl = document.createElement("p");
  errEl.className = "nf-error";
  errEl.hidden = true;

  const actions = document.createElement("div");
  actions.className = "nf-actions";

  const cancelBtn = document.createElement("button");
  cancelBtn.type = "button";
  cancelBtn.className = "nf-btn";
  cancelBtn.textContent = "Cancel";
  cancelBtn.addEventListener("click", close);

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
    connectCommandsSection,
    errEl,
    actions,
  );

  dialog.appendChild(form);

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
    const connectCommands = connectCommandsEl.value
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean);

    if (!(name && host && port && nick)) {
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
      connect_commands: connectCommands,
    };
    if (isEdit) {
      body.disabled = disabledEl.checked;
      // On edit, blank SASL fields mean "keep existing" (backend contract), so
      // only send them when the user actually typed a value. This prevents an
      // edit that doesn't touch SASL from wiping stored credentials.
      if (saslUser) body.sasl_user = saslUser;
      if (saslPass) body.sasl_pass = saslPass;
    } else {
      body.sasl_user = saslUser;
      body.sasl_pass = saslPass;
    }

    submitBtn.disabled = true;
    submitBtn.textContent = isEdit ? "Saving…" : "Adding…";

    try {
      const url = existing ? `/api/networks/${existing.id}` : "/api/networks";
      const data = await sendJSON<FormResult>(url, isEdit ? "PATCH" : "POST", body);
      state.networks.set(data.id, data);
      close();
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
