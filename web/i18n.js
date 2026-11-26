"use strict";
// Musaarr's words in the reader's language. Each language is one flat catalogue,
// web/i18n/<lang>.json: "status.waiting": "Waiting for a source". {holes} are filled from params,
// and a count picks key_one, key_other (or key_many…) with the language's own plural rules.
// English is the source and the fallback; a missing key shows itself, so a test can catch it.
(() => {
  const LANGS = { en: "English", fr: "Français" };
  const KEY = "musaarr.lang";

  function chosen() {
    let saved = null;
    try { saved = localStorage.getItem(KEY); } catch { /* private window */ }
    for (const l of [saved, ...(navigator.languages || [navigator.language])]) {
      const base = String(l || "").toLowerCase().split("-")[0];
      if (base in LANGS) return base;
    }
    return "en";
  }

  const lang = chosen();
  document.documentElement.lang = lang;
  const rules = new Intl.PluralRules(lang);
  const nums = new Map();
  let words = {}, english = {};

  const fill = (s, p) => (p ? s.replace(/\{(\w+)\}/g, (hole, k) => (p[k] ?? hole)) : s);
  const has = (key) => key in words || key in english;

  // t is the sentence for key, its {holes} filled from params.
  function t(key, params) { return fill(words[key] ?? english[key] ?? key, params); }

  // tn is t for a count: {n} is n written the reader's way, and the plural form is the language's.
  function tn(key, n, params) {
    const form = `${key}_${rules.select(n)}`;
    return t(has(form) ? form : `${key}_other`, { ...params, n: num(n) });
  }

  // num writes n the reader's way (1 234,5 in French), with at most digits decimals.
  function num(n, digits = 0) {
    if (!nums.has(digits)) nums.set(digits, new Intl.NumberFormat(lang, { maximumFractionDigits: digits }));
    return nums.get(digits).format(n);
  }

  // localize fills the page's fixed words: data-i18n is the text, data-i18n-<attr> an attribute.
  function localize(root = document) {
    for (const tpl of root.querySelectorAll("template")) localize(tpl.content);
    for (const el of root.querySelectorAll("[data-i18n]")) el.textContent = t(el.dataset.i18n);
    for (const attr of ["aria-label", "aria-roledescription", "placeholder", "title", "alt", "src"]) {
      for (const el of root.querySelectorAll(`[data-i18n-${attr}]`)) el.setAttribute(attr, t(el.getAttribute(`data-i18n-${attr}`)));
    }
  }

  function setLang(l) {
    try { localStorage.setItem(KEY, l); } catch { /* the choice then lasts this visit only */ }
    location.reload();
  }

  const load = (l) => fetch(`/i18n/${l}.json`).then((r) => (r.ok ? r.json() : {})).catch(() => ({}));
  const ready = Promise.all([load(lang), lang === "en" ? null : load("en")]).then(([w, en]) => {
    words = w;
    english = en || w;
    localize();
  }).finally(() => document.documentElement.removeAttribute("data-i18n-wait"));

  Object.assign(window, { t, tn, num, localize, setLang, i18nReady: ready, i18n: { lang, langs: LANGS, has } });
})();
