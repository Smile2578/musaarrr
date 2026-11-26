"use strict";
// The settings page, and the first-run wizard when Lidarr is not set yet: Lidarr tested and its
// profiles picked from lists, Plex by a code approved on plex.tv, then the optional extras.
// Keys and tokens are sent, never shown back: a saved one is only said to be there.

const $ = (s) => document.querySelector(s);
let view = null;   // GET /api/settings
let lidarr = null; // the last successful Lidarr test
let pin = null;    // the Plex code waiting for approval: { id, timer, until }

// errorText reads a server error: its code's sentence, else the English one it carries.
function errorText(data) {
  if (data.code && i18n.has(`error.${data.code}`)) return t(`error.${data.code}`, data.params);
  return data.error || t("auth.unreachable");
}

// call sends one request; a lost session goes back to the sign-in page.
async function call(method, path, body) {
  const file = body instanceof Blob; // a backup to restore goes as it is
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": file ? "application/zip" : "application/json" } : {},
    body: file ? body : body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 401) {
    location.replace("/login.html");
    return new Promise(() => {});
  }
  let data = {};
  try { data = await res.json(); } catch { /* empty body */ }
  return { ok: res.ok, data };
}

function say(id, text, kind) {
  const box = $(id);
  box.textContent = text || "";
  box.classList.toggle("is-ok", kind === "ok");
  box.classList.toggle("is-error", kind === "error");
}

// busy disables a button while its request runs and gives it the matching word.
function busy(button, on, word) {
  button.disabled = on;
  button.textContent = t(word);
  button.closest("form, section")?.setAttribute("aria-busy", String(on));
}

function invalid(input, on) {
  if (on) input.setAttribute("aria-invalid", "true");
  else input.removeAttribute("aria-invalid");
}

// setField shows a setting: a fixed one cannot be typed into, a saved secret only says it is there.
function setField(input, key, hint) {
  const v = view.values[key] || {};
  const secret = input.type === "password";
  if (!secret) input.value = v.value || "";
  input.readOnly = !!v.locked;
  input.placeholder = secret && v.set ? t("setup.secretSaved") : "";
  $(`#${input.getAttribute("aria-describedby")}`).textContent = v.locked ? t("setup.locked", { key }) : hint;
}

function render(next) {
  view = next;
  const ready = view.ready;
  $("#title").textContent = t(ready ? "setup.heading.ready" : "setup.heading.first");
  $("#lead").textContent = t(ready ? "setup.lead.ready" : "setup.lead.first");
  document.title = `${t("setup.title")} · Musaarr`;
  setField($("#lidarr-url"), "LIDARR_URL", t("setup.lidarr.urlHint"));
  setField($("#lidarr-key"), "LIDARR_API_KEY", t("setup.lidarr.keyHint"));
  setField($("#listenbrainz"), "LISTENBRAINZ_TOKEN", t("setup.more.listenbrainzHint"));
  setField($("#prowlarr-url"), "PROWLARR_URL", t("setup.more.prowlarrHint"));
  setField($("#prowlarr-key"), "PROWLARR_API_KEY", t("setup.more.prowlarrHint"));
  setField($("#hires-tags"), "HIRES_TAGS", t("setup.more.hiResTagsHint"));
  setField($("#multichannel-tags"), "MULTICHANNEL_TAGS", t("setup.more.multichannelTagsHint"));
  for (const id of ["#done", "#plex", "#more"]) $(id).hidden = !ready;
  const plex = view.values.PLEX_URL || {};
  $("#plex-now").hidden = !plex.value;
  $("#plex-now").textContent = plex.value ? t("setup.plex.connected", { url: plex.value }) : "";
  $("#plex-off").hidden = !plex.value || plex.locked;
  $("#plex-start").hidden = plex.locked;
}

async function load() {
  $("#offline").hidden = true;
  $("#wait").hidden = false;
  let res;
  try {
    res = await call("GET", "/api/settings");
  } catch {
    res = { ok: false, data: {} };
  }
  $("#wait").hidden = true;
  $("#setup").setAttribute("aria-busy", "false");
  if (!res.ok) {
    $("#offline-text").textContent = res.data.code === "auth.adminOnly" ? t("setup.forbidden") : errorText(res.data);
    $("#retry").hidden = res.data.code === "auth.adminOnly";
    $("#offline").hidden = false;
    return;
  }
  render(res.data);
  $("#page").hidden = false;
  $(view.ready ? "#title" : "#lidarr-url").focus?.();
}

// options fills a select and picks want when Lidarr has it, else the first.
function options(select, names, want) {
  select.replaceChildren(...names.map((n) => new Option(n, n)));
  select.value = names.includes(want) ? want : names[0] || "";
}

function fillChoices() {
  const roots = lidarr.rootFolders.map((r) => r.path);
  const root = lidarr.rootFolders.find((r) => r.path === view.values.ROOT_FOLDER?.value) || lidarr.rootFolders[0] || {};
  options($("#root"), roots, root.path);
  options($("#quality"), lidarr.qualityProfiles, view.values.QUALITY_PROFILE?.value || root.qualityProfile);
  options($("#metadata"), lidarr.metadataProfiles, view.values.METADATA_PROFILE?.value || root.metadataProfile);
  for (const [id, key] of [["#root", "ROOT_FOLDER"], ["#quality", "QUALITY_PROFILE"], ["#metadata", "METADATA_PROFILE"]]) {
    $(id).disabled = !!view.values[key]?.locked;
  }
  $("#choice-save").disabled = !roots.length;
  say("#choice-status", roots.length ? "" : t("setup.lidarr.noRoot"), roots.length ? "" : "error");
  $("#choice-form").hidden = false;
}

async function testLidarr(e) {
  e.preventDefault();
  const url = $("#lidarr-url"), key = $("#lidarr-key"), button = $("#lidarr-test");
  invalid(url, false);
  invalid(key, false);
  if (!/^https?:\/\/\S+$/i.test(url.value.trim())) {
    invalid(url, true);
    url.focus();
    return say("#lidarr-status", t("error.setup.badUrl"), "error");
  }
  busy(button, true, "setup.lidarr.testing");
  say("#lidarr-status", "");
  let res;
  try {
    res = await call("POST", "/api/settings/lidarr/test", { url: url.value.trim(), apiKey: key.value.trim() });
  } catch {
    res = { ok: false, data: {} };
  }
  busy(button, false, "setup.lidarr.test");
  if (!res.ok) {
    const field = res.data.code === "setup.lidarrBadKey" ? key : url;
    invalid(field, true);
    field.focus();
    $("#choice-form").hidden = true;
    return say("#lidarr-status", errorText(res.data), "error");
  }
  lidarr = res.data;
  say("#lidarr-status", t("setup.lidarr.ok", { version: lidarr.version }), "ok");
  fillChoices();
  $("#root").focus();
}

// changes are the values a form sends: fixed ones are left out, and an empty secret keeps the saved one.
function changes(pairs) {
  const out = {};
  for (const [key, value, secret] of pairs) {
    if (view.values[key]?.locked || (secret && !value)) continue;
    out[key] = value;
  }
  return out;
}

// save sends values and shows the outcome in status; it returns whether they were kept.
async function save(values, button, status) {
  if (!Object.keys(values).length) return say(status, t("setup.saved"), "ok"), true;
  busy(button, true, "setup.saving");
  let res;
  try {
    res = await call("PUT", "/api/settings", { values });
  } catch {
    res = { ok: false, data: {} };
  }
  busy(button, false, "setup.save");
  if (!res.ok) return say(status, errorText(res.data), "error"), false;
  const first = !view.ready && res.data.ready;
  render(res.data);
  say(status, t("setup.saved"), "ok");
  if (first) $("#done a").focus();
  return true;
}

function saveLidarr(e) {
  e.preventDefault();
  save(changes([
    ["LIDARR_URL", $("#lidarr-url").value.trim()],
    ["LIDARR_API_KEY", $("#lidarr-key").value.trim(), true],
    ["ROOT_FOLDER", $("#root").value],
    ["QUALITY_PROFILE", $("#quality").value],
    ["METADATA_PROFILE", $("#metadata").value],
  ]), $("#choice-save"), "#choice-status").then((ok) => { if (ok) $("#lidarr-key").value = ""; });
}

async function saveMore(e) {
  e.preventDefault();
  const inputs = [...$("#more-form").querySelectorAll("[data-key]")];
  const ok = await save(changes(inputs.map((i) => [i.dataset.key, i.value.trim(), i.type === "password"])), $("#more-save"), "#more-status");
  if (ok) for (const i of inputs) if (i.type === "password") i.value = "";
}

/* ---------- Plex, by a code approved on plex.tv ---------- */

function stopPin() {
  if (pin) clearTimeout(pin.timer);
  pin = null;
  document.removeEventListener("visibilitychange", pollPin);
}

async function startPlex() {
  const button = $("#plex-start");
  stopPin();
  $("#plex-servers").hidden = true;
  say("#plex-status", "");
  busy(button, true, "setup.plex.starting");
  let res;
  try {
    res = await call("POST", "/api/settings/plex/pin");
  } catch {
    res = { ok: false, data: {} };
  }
  busy(button, false, "setup.plex.start");
  if (!res.ok) return say("#plex-status", errorText(res.data), "error");
  pin = { id: res.data.id, until: Date.now() + 14 * 60 * 1000 };
  $("#plex-code").textContent = res.data.code;
  $("#plex-wait").textContent = t("setup.plex.code");
  $("#plex-link").href = res.data.url;
  $("#plex-pin").hidden = false;
  $("#plex-link").focus();
  document.addEventListener("visibilitychange", pollPin); // back from the plex.tv tab: look at once
  pin.timer = setTimeout(pollPin, 2000);
}

// pollPin asks every 2 s whether the code was approved, for as long as it lives.
async function pollPin() {
  if (!pin || document.hidden) return;
  clearTimeout(pin.timer);
  const id = pin.id;
  let res;
  try {
    res = await call("GET", `/api/settings/plex/pin/${id}`);
  } catch {
    res = { ok: true, data: { done: false } }; // a blip: the next look may get through
  }
  if (!pin || pin.id !== id) return;
  if (!res.ok || (!res.data.done && Date.now() > pin.until)) {
    stopPin();
    $("#plex-pin").hidden = true;
    return say("#plex-status", res.ok ? t("error.setup.plexPinUnknown") : errorText(res.data), "error");
  }
  if (!res.data.done) {
    pin.timer = setTimeout(pollPin, 2000);
    return;
  }
  $("#plex-pin").hidden = true;
  showServers(res.data.servers || []);
}

function serverChoice(server, c, checked) {
  const label = document.createElement("label");
  label.className = "choice";
  const input = Object.assign(document.createElement("input"), { type: "radio", name: "plex-uri", value: c.uri, checked });
  input.dataset.server = server.id;
  const text = document.createElement("span");
  text.className = "choice__text";
  const name = Object.assign(document.createElement("span"), { className: "choice__name", textContent: server.name });
  const uri = Object.assign(document.createElement("span"), { className: "choice__uri", textContent: c.uri });
  const where = t(c.relay ? "setup.plex.relay" : c.local ? "setup.plex.local" : "setup.plex.remote");
  const tags = Object.assign(document.createElement("span"), {
    className: `choice__tags ${c.ok ? "is-ok" : ""}`,
    textContent: `${where} · ${t(c.ok ? "setup.plex.answers" : "setup.plex.silent")}`,
  });
  text.append(name, uri, tags);
  label.append(input, text);
  return label;
}

function showServers(servers) {
  const list = $("#plex-list");
  const conns = servers.flatMap((s) => s.connections.map((c) => [s, c]));
  const best = Math.max(conns.findIndex(([, c]) => c.ok), 0);
  list.replaceChildren(...conns.map(([s, c], i) => serverChoice(s, c, i === best)));
  if (!conns.length) return say("#plex-status", t("setup.plex.none"), "error");
  const answers = conns[best][1].ok;
  say("#plex-status", answers ? "" : t("setup.plex.noneAnswers"), answers ? "" : "error");
  $("#plex-servers").hidden = false;
  list.querySelector("input:checked").focus();
}

async function choosePlex(e) {
  e.preventDefault();
  const picked = $("#plex-list input:checked");
  if (!picked || !pin) return;
  const button = $("#plex-save");
  busy(button, true, "setup.saving");
  let res;
  try {
    res = await call("POST", "/api/settings/plex/server", { pin: pin.id, server: picked.dataset.server, uri: picked.value });
  } catch {
    res = { ok: false, data: {} };
  }
  busy(button, false, "setup.plex.use");
  if (!res.ok) return say("#plex-status", errorText(res.data), "error");
  stopPin();
  $("#plex-servers").hidden = true;
  render(res.data);
  say("#plex-status", "");
  $("#plex-now").focus();
}

async function plexOff() {
  if (await save({ PLEX_URL: "", PLEX_TOKEN: "" }, $("#plex-off"), "#plex-status")) $("#plex-start").focus();
  $("#plex-off").textContent = t("setup.plex.disconnect");
}

/* ---------- Backup ---------- */

// restore asks twice: the first press says what will be replaced, the second restores.
async function restore(e) {
  e.preventDefault();
  const input = $("#restore-file"), button = $("#restore-go");
  if (!input.files.length) {
    input.focus();
    return say("#restore-status", t("setup.backup.pick"), "error");
  }
  if (!button.classList.contains("is-armed")) {
    button.classList.add("is-armed");
    button.textContent = t("setup.backup.confirm");
    return say("#restore-status", t("setup.backup.warn"), "error");
  }
  button.classList.remove("is-armed");
  busy(button, true, "setup.backup.restoring");
  let res;
  try {
    res = await call("POST", "/api/settings/restore", input.files[0]);
  } catch {
    res = { ok: false, data: {} };
  }
  busy(button, false, "setup.backup.restore");
  if (!res.ok) return say("#restore-status", errorText(res.data), "error");
  say("#restore-status", t("setup.backup.done"), "ok");
  setTimeout(() => location.replace("/setup.html"), 1500); // the backup's accounts may not hold this one
}

function disarm() {
  $("#restore-go").classList.remove("is-armed");
  $("#restore-go").textContent = t("setup.backup.restore");
  say("#restore-status", "");
}

i18nReady.then(() => {
  $("#restore-form").addEventListener("submit", restore);
  $("#restore-file").addEventListener("change", disarm);
  $("#lidarr-form").addEventListener("submit", testLidarr);
  $("#choice-form").addEventListener("submit", saveLidarr);
  $("#more-form").addEventListener("submit", saveMore);
  $("#plex-start").addEventListener("click", startPlex);
  $("#plex-servers").addEventListener("submit", choosePlex);
  $("#plex-off").addEventListener("click", plexOff);
  $("#retry").addEventListener("click", load);
  $("#root").addEventListener("change", () => {
    const root = lidarr.rootFolders.find((r) => r.path === $("#root").value);
    if (root?.qualityProfile && !$("#quality").disabled) $("#quality").value = root.qualityProfile;
  });
  load();
});
