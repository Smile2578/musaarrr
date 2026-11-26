"use strict";
// The sign-in page: the first visit creates the admin account, the next ones sign in.
// A person a trusted proxy already names never sees it: the server lets them straight in.

const $ = (s) => document.querySelector(s);
let setup = false;

// errorText reads a server error: its code's sentence, else the English one it carries.
function errorText(data) {
  if (data.code && i18n.has(`error.${data.code}`)) return t(`error.${data.code}`, data.params);
  return data.error || t("auth.unreachable");
}

function busy(on) {
  $("#submit").disabled = on;
  $("#submit").textContent = t(setup ? (on ? "auth.setup.busy" : "auth.setup.submit") : (on ? "auth.login.busy" : "auth.login.submit"));
  $("#form").setAttribute("aria-busy", String(on));
}

function showError(text, field) {
  const box = $("#error");
  box.textContent = text;
  box.hidden = !text;
  for (const input of [$("#name"), $("#password")]) {
    if (input === field) input.setAttribute("aria-invalid", "true");
    else input.removeAttribute("aria-invalid");
  }
  if (field) field.focus();
}

async function load() {
  $("#offline").hidden = true;
  $("#wait").hidden = false;
  let state;
  try {
    const res = await fetch("/api/auth", { headers: { Accept: "application/json" } });
    if (!res.ok) throw new Error(String(res.status));
    state = await res.json();
  } catch {
    $("#wait").hidden = true;
    $("#offline").hidden = false;
    $("#signin").setAttribute("aria-busy", "false");
    $("#retry").focus();
    return;
  }
  if (state.user) {
    location.replace("/");
    return;
  }
  setup = state.setup;
  $("#title").textContent = t(setup ? "auth.setup.heading" : "auth.login.heading");
  $("#lead").textContent = t(setup ? "auth.setup.lead" : "auth.login.lead");
  $("#password").autocomplete = setup ? "new-password" : "current-password";
  $("#password-hint").hidden = !setup;
  document.title = `${t(setup ? "auth.setup.submit" : "auth.login.submit")} · Musaarr`;
  busy(false);
  $("#wait").hidden = true;
  $("#form").hidden = false;
  $("#signin").setAttribute("aria-busy", "false");
  $("#name").focus();
}

async function submit(e) {
  e.preventDefault();
  const name = $("#name").value.trim();
  const password = $("#password").value;
  if (!name) return showError(t("error.auth.badName"), $("#name"));
  if (setup && password.length < 8) return showError(t("error.auth.passwordTooShort"), $("#password"));
  if (!password) return showError(t("error.auth.badCredentials"), $("#password"));
  showError("");
  busy(true);
  let res, data = {};
  try {
    res = await fetch(setup ? "/api/auth/setup" : "/api/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, password }),
    });
    try { data = await res.json(); } catch { /* empty body */ }
  } catch {
    busy(false);
    return showError(t("auth.unreachable"));
  }
  if (res.ok) {
    location.replace("/");
    return;
  }
  busy(false);
  if (data.code === "auth.alreadySetUp") { // someone else just created the admin: this page becomes the sign-in
    await load();
    return showError(errorText(data));
  }
  const field = data.code === "auth.badName" ? $("#name") : $("#password");
  if (data.code === "auth.badCredentials") $("#password").select();
  showError(errorText(data), field);
}

function togglePassword() {
  const shown = $("#password").type === "text";
  $("#password").type = shown ? "password" : "text";
  $("#show").textContent = t(shown ? "auth.showPassword" : "auth.hidePassword");
}

i18nReady.then(() => {
  $("#form").addEventListener("submit", submit);
  $("#show").addEventListener("click", togglePassword);
  $("#retry").addEventListener("click", load);
  load();
});
