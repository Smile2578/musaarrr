"use strict";
// The account in the app's header: « Réglages » for an admin, « Se déconnecter » for whoever
// signed in with a password. Someone a trusted proxy names (Tailscale, Authelia…) has nothing
// to sign out of and sees no such button.

i18nReady.then(async () => {
  const button = document.querySelector("#signout");
  let state;
  try {
    state = await (await fetch("/api/auth")).json();
  } catch {
    return; // offline: the banner says so, and signing out can wait
  }
  const settings = document.querySelector("#settings");
  if (settings && state.user?.admin) settings.hidden = false;
  if (!state.user || state.user.proxy) return;
  button.textContent = t("auth.logout");
  button.hidden = false;
  button.addEventListener("click", async () => {
    button.disabled = true;
    try {
      const res = await fetch("/api/auth/logout", { method: "POST" });
      if (!res.ok) throw new Error(String(res.status));
    } catch {
      button.disabled = false;
      toast(t("auth.unreachable"));
      return;
    }
    location.replace("/login.html");
  });
});
