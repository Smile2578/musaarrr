"use strict";

// Words come from web/i18n (t, tn, num): see i18n.js.
const statusLabel = (s) => (i18n.has(`status.${s}`) ? t(`status.${s}`) : s);
const EXPLAINED = new Set(["waiting", "check", "imported", "picked"]);
const ACTIVE = new Set(["requested", "searching", "downloading", "importing", "imported", "picked"]); // imported, picked: on their way to Plex
const PULSING = new Set(["requested", "searching", "importing"]);
const GROUPS = ["albums", "ep", "singles", "others"];

const $ = (s) => document.querySelector(s);
const state = {
  tab: "search",
  requests: new Map(),
  listKey: "",
  shown: new Map(), // mbid -> { status, p } last drawn on the requests tab, to animate the fill forward
  timer: 0,
  controller: null,
  lastQuery: "",
  artists: new Map(), // artists seen in search results, for the name and portrait before the page loads
  view: "search", // the Search tab's page: search, artist, album or sources
  artist: null, // { mbid, name, names, items, group, sources }; sources: { status, ask, data, error }
  artistController: null,
  artistSourcesController: null,
  album: null, // the album page's mbid
  albumData: null, // /api/albums answer for state.album: { album, editions, owned }
  albumSources: null, // the open album's last tracker answer, for the line that offers a better version
  editionFilter: "*", // the album page's releases shown: "*" all, an edition's key, "" those to confirm
  albumController: null,
  albumSearch: "", // the album whose own tracker search runs or shows
  albumAsk: "", // the artist's name that search went under
  albumFromArtist: false,
  navigated: false,
  returnTab: null,
  fromLibrary: false, // the album page was opened from the Library: its back link returns there
  fromShop: false, // the album or artist page was opened from the shop's crate: Back returns to it
  fresh: new Set(), // albums that reached Plex since the last list: their green band inks in again
  searchScroll: 0,
  artistScroll: 0,
  order: "listens", // the artist's albums: most listened first, or by date
};

async function api(path, options = {}) {
  let res;
  try {
    res = await fetch(path, { ...options, headers: options.body ? { "Content-Type": "application/json" } : undefined });
  } catch (err) {
    if (err.name === "AbortError") throw err;
    throw Object.assign(new Error(t("error.unreachable")), { away: true });
  }
  let data = {};
  try { data = await res.json(); } catch { /* empty or non-JSON body */ }
  if (res.status === 401) { // the session ended: sign in again, and never resolve, since the page leaves
    location.replace("/login.html");
    return new Promise(() => {});
  }
  if (!res.ok) throw Object.assign(new Error(errorText(data)), { status: res.status, code: data.code, away: !data.error });
  return data;
}

// errorText is the server's error in the reader's language: its code's sentence, else its English fallback.
function errorText(data) {
  if (data.code && i18n.has(`error.${data.code}`)) return t(`error.${data.code}`, data.params);
  return data.error || t("error.unreachable");
}

function el(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

function toast(message) {
  const box = $("#toast");
  box.textContent = message;
  box.classList.add("is-visible");
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => box.classList.remove("is-visible"), 5000);
}

function announce(message) {
  const a = $("#announcer");
  a.textContent = "";
  requestAnimationFrame(() => { a.textContent = message; });
}

function notice(prefix, text, retry) {
  const box = $(`#${prefix}-notice`);
  box.hidden = !text;
  box.classList.toggle("is-error", Boolean(retry));
  $(`#${prefix}-hint`).textContent = text || "";
  const button = $(`#${prefix}-retry`);
  button.hidden = !retry;
  button.onclick = retry || null;
}

function typeLabel(item) {
  const sec = item.secondary || [];
  for (const s of ["Live", "Compilation", "Remix", "Soundtrack"]) if (sec.includes(s)) return t(`type.${s.toLowerCase()}`);
  return t({ Album: "type.album", EP: "type.ep", Single: "type.single" }[item.type] || "type.other");
}

function subline(item) {
  return [item.artist, item.year || "", typeLabel(item)].filter(Boolean).join(" · ");
}

function setCover(sleeve, item) {
  const none = () => sleeve.replaceChildren(el("span", "sleeve__none", t("cover.none")));
  if (!item.cover) { none(); return; }
  const img = el("img", "cover");
  img.alt = ""; // the title sits right under the cover
  img.loading = "lazy";
  img.decoding = "async";
  img.addEventListener("error", none, { once: true });
  img.src = item.cover;
  sleeve.replaceChildren(img);
}

function setPortrait(box, artist) {
  const initial = () => {
    const letter = el("span", "portrait__initial", ([...artist.name.trim()][0] || "?").toUpperCase());
    letter.setAttribute("aria-hidden", "true");
    box.replaceChildren(letter);
  };
  if (!artist.image) { initial(); return; }
  const img = el("img");
  img.alt = "";
  img.loading = "lazy";
  img.decoding = "async";
  img.addEventListener("error", initial, { once: true });
  img.src = artist.image;
  box.replaceChildren(img);
}

function statusText(item) {
  if (item.status === "downloading") return t("status.downloadingAt", { p: item.progress });
  return item.status ? statusLabel(item.status) : "";
}

// Lidarr gives "hh:mm:ss" (sometimes "d.hh:mm:ss"); show a short duration.
function eta(left) {
  const m = /^(?:(\d+)\.)?(\d+):(\d+):(\d+)/.exec(left);
  if (!m) return left;
  const mins = (+(m[1] || 0)) * 1440 + (+m[2]) * 60 + (+m[3]);
  if (mins < 1) return t("time.underMin");
  if (mins < 60) return t("time.min", { n: mins });
  return t("time.hm", { h: Math.floor(mins / 60), m: String(mins % 60).padStart(2, "0") });
}

/* ---------- Status band ---------- */

function bandLabel(item, big) {
  const label = el("span", "band__label");
  if (PULSING.has(item.status)) label.append(el("span", `band__mark${item.status === "searching" ? " is-live" : ""}`));
  if (item.status === "downloading") {
    label.append(el("span", "", statusLabel("downloading")));
    if (big) { // a card's band stays on one line: its fill shows the progress, the figures live in En route
      const left = item.timeleft ? ` · ≈ ${eta(item.timeleft)}` : "";
      label.append(el("span", "band__aside", `${t("pct", { p: item.progress })}${left}`));
    }
  } else {
    label.append(el("span", "", statusLabel(item.status)));
  }
  return label;
}

const setFill = (node, p) => node.style.setProperty("--p", `${p}%`);

// band draws a state; `from` animates the fill forward from an earlier percentage.
function band(item, { big = false, tag = "div", fresh = false, from } = {}) {
  const node = el(tag, `band s-${item.status}${big ? " band--big" : ""}${fresh ? " is-new" : ""}`);
  const fill = el("span", "band__fill");
  fill.setAttribute("aria-hidden", "true");
  fill.append(bandLabel(item, big));
  node.append(bandLabel(item, big), fill);
  const p = item.status === "downloading" ? item.progress : 100;
  if (from !== undefined && from < p) {
    setFill(node, from);
    requestAnimationFrame(() => requestAnimationFrame(() => setFill(node, p)));
  } else {
    setFill(node, p);
  }
  if (item.status === "downloading") {
    node.setAttribute("role", "progressbar");
    node.setAttribute("aria-valuemin", "0");
    node.setAttribute("aria-valuemax", "100");
    node.setAttribute("aria-valuenow", String(item.progress));
    node.setAttribute("aria-label", t("band.downloading", { title: item.title }));
    if (item.timeleft) node.setAttribute("aria-valuetext", t("band.eta", { p: item.progress, eta: eta(item.timeleft) }));
  }
  return node;
}

/* ---------- Album cards ---------- */

function cardAction(item) {
  const box = el("div", "card__action");
  if (item.status === "in_plex" && item.plexUrl) {
    const link = band(item, { tag: "a" });
    link.href = item.plexUrl;
    link.target = "_blank";
    link.rel = "noopener";
    link.setAttribute("aria-label", t("card.openInPlex", { title: item.title }));
    box.append(link);
  } else if (item.status) {
    box.append(band(item));
  } else {
    const b = el("button", "btn btn--blue", t("card.request"));
    b.type = "button";
    b.setAttribute("aria-label", t("card.requestLabel", { title: item.title, artist: item.artist }));
    b.addEventListener("click", () => request(item, box));
    box.append(b);
  }
  return box;
}

async function request(item, box) {
  const button = box.firstChild;
  const hadFocus = document.activeElement === button;
  fly(button.getBoundingClientRect());
  const inked = band({ ...item, status: "requested" }, { fresh: true });
  inked.tabIndex = -1;
  box.replaceChildren(inked);
  if (hadFocus) inked.focus();
  announce(t("card.requested", { title: item.title }));
  try {
    const saved = await api("/api/requests", { method: "POST", body: JSON.stringify({ mbid: item.mbid }) });
    item.status = saved.status;
    state.requests.set(saved.mbid, saved);
    updateBadge();
    renderRecent();
  } catch (err) {
    box.replaceChildren(button);
    if (hadFocus) button.focus();
    toast(err.message);
  }
}

const compact = new Intl.NumberFormat(i18n.lang, { notation: "compact", maximumFractionDigits: 1 });

function listensText(n) {
  if (!n) return "";
  return n >= 1e6 ? t("listens.millions", { c: compact.format(n) }) : tn("listens.count", n, { c: compact.format(n) });
}

// renderAlbums draws album cards; with link, each opens its album page; on an artist's page it also
// shows its best release on the trackers.
function renderAlbums(list, items, { withArtist = true, link = false, artistPage = false } = {}) {
  const tpl = $("#tpl-card");
  list.replaceChildren(...items.map((item) => {
    const node = tpl.content.firstElementChild.cloneNode(true);
    const sleeve = node.querySelector(".sleeve");
    setCover(sleeve, item);
    const title = node.querySelector(".card__title");
    title.textContent = item.title;
    const sub = node.querySelector(".card__sub");
    if (withArtist) sub.textContent = item.artist;
    else sub.remove();
    // The type goes where nothing else says it: the artist page has its segments, the incontournables are albums.
    const typed = group(item) === "others" || !(artistPage || item.listens);
    node.querySelector(".card__meta").textContent = [item.year || "", typed ? typeLabel(item) : "", listensText(item.listens)].filter(Boolean).join(" · ");
    if ((link && item.artistMbid) || artistPage) {
      const href = `#artiste/${item.artistMbid || state.artist.mbid}/album/${item.mbid}`;
      const link = el("a", "card__link", item.title);
      link.href = href;
      link.dataset.album = item.mbid;
      title.replaceChildren(link);
      const cover = el("a", "card__cover"); // a second way in for the finger; the title link is the one read out
      cover.href = href;
      cover.tabIndex = -1;
      cover.setAttribute("aria-hidden", "true");
      sleeve.replaceWith(cover);
      cover.append(sleeve);
    }
    if (artistPage) {
      const quality = el("p", "card__quality");
      quality.dataset.mbid = item.mbid;
      fillQuality(quality);
      node.querySelector(".card__meta").after(quality);
    }
    node.querySelector(".card__action").replaceWith(cardAction(item));
    return node;
  }));
}

function skeleton(list, n) {
  const tpl = $("#tpl-card");
  list.replaceChildren(...Array.from({ length: n }, () => {
    const node = tpl.content.firstElementChild.cloneNode(true);
    node.classList.add("skeleton");
    node.querySelector(".card__action").remove();
    node.setAttribute("aria-hidden", "true");
    return node;
  }));
}

// loadSuggestions shows, on the search page, the most listened albums of your artists that Plex lacks.
async function loadSuggestions() {
  try {
    const data = await api("/api/suggestions");
    const items = data.results || [];
    $("#suggest-block").hidden = items.length === 0;
    renderAlbums($("#suggest"), items, { link: true });
  } catch { /* the search page works without them */ }
}

/* ---------- Search ---------- */

function renderArtists(artists) {
  const tpl = $("#tpl-artist");
  $("#artists").replaceChildren(...artists.map((a) => {
    state.artists.set(a.mbid, a);
    const node = tpl.content.firstElementChild.cloneNode(true);
    node.querySelector(".artist").href = `#artiste/${a.mbid}`;
    setPortrait(node.querySelector(".portrait"), a);
    node.querySelector(".artist__name").textContent = a.name;
    const dis = node.querySelector(".artist__dis");
    if (a.disambiguation) dis.textContent = a.disambiguation;
    else dis.remove();
    return node;
  }));
  $("#artists-block").hidden = artists.length === 0;
}

function resetSearch(text) {
  $("#prompt").hidden = Boolean(text);
  $("#artists-block").hidden = true;
  $("#albums-block").hidden = true;
  $("#results").replaceChildren();
  notice("search", text);
}

async function search(q) {
  state.controller?.abort();
  offerTrackers("");
  if ([...q].length < 2) {
    stageSay("search", false);
    resetSearch(q ? t("search.tooShort") : "");
    return;
  }
  const controller = new AbortController();
  state.controller = controller;
  const results = $("#results");
  $("#prompt").hidden = true;
  notice("search", "");
  $("#artists-block").hidden = true;
  $("#albums-block").hidden = false;
  skeleton(results, 6);
  results.setAttribute("aria-busy", "true");
  stageSay("search", true);
  try {
    const data = await api(`/api/search?q=${encodeURIComponent(q)}`, { signal: controller.signal });
    renderArtists(data.artists || []);
    renderAlbums(results, data.results, { link: true });
    $("#albums-block").hidden = data.results.length === 0;
    offerTrackers(q);
    if (!data.results.length && !(data.artists || []).length) {
      notice("search", t("search.none", { q }));
    }
  } catch (err) {
    if (err.name === "AbortError") return;
    resetSearch("");
    $("#prompt").hidden = true;
    notice("search", err.message, () => search(q));
  } finally {
    if (state.controller === controller) {
      results.setAttribute("aria-busy", "false");
      stageSay("search", false);
    }
  }
}

let debounce = 0;
function onInput() {
  const q = $("#q").value.trim();
  if (q === state.lastQuery) return;
  state.lastQuery = q;
  if (state.view === "artist" || state.view === "album") { // typing goes back to the search; Back returns to the page
    state.returnTab = null;
    state.fromLibrary = false;
    state.fromShop = false;
    history.pushState(null, "", location.pathname + location.search);
    route();
  }
  clearTimeout(debounce);
  debounce = setTimeout(() => search(q), 300);
}

/* ---------- Artist page ---------- */

const VIEWS = ["search", "artist", "album", "sources"];

// view shows one of the Search tab's pages and keeps where the page left behind was scrolled.
function view(name) {
  if (state.view === name) return;
  if (state.view === "search") state.searchScroll = window.scrollY;
  if (state.view === "artist") state.artistScroll = window.scrollY;
  state.view = name;
  for (const v of VIEWS) $(`#${v}-view`).hidden = v !== name;
  $("#search-form").hidden = name === "sources"; // that page has its own field, run on its Search button only
}

function group(item) {
  if ((item.secondary || []).length) return "others";
  return { Album: "albums", EP: "ep", Single: "singles" }[item.type] || "others";
}

function openArtist(mbid) {
  const fromAlbum = state.view === "album";
  view("artist");
  if (state.artist?.mbid === mbid && state.artist.items) {
    setArtistName(state.artist.name);
    if (!state.artist.sources) loadArtistSources();
    if (fromAlbum) { // back on the card that was opened, where it was
      document.querySelector(`#discography [data-album="${CSS.escape(state.album || "")}"]`)?.focus({ preventScroll: true });
      requestAnimationFrame(() => window.scrollTo(0, state.artistScroll));
    }
    return;
  }
  startArtist(mbid);
  window.scrollTo(0, 0);
  $("#artist-name").focus({ preventScroll: true });
}

// startArtist loads an artist's albums and, for its page, at the same time its releases on the trackers.
function startArtist(mbid, trackers = true) {
  const known = state.artists.get(mbid);
  const kept = state.artist?.mbid === mbid ? state.artist.sources : null;
  state.artist = { mbid, name: known?.name || "", items: null, group: null, sources: kept };
  const portrait = $("#artist-portrait");
  portrait.hidden = !known;
  if (known) setPortrait(portrait, known);
  setArtistName(known?.name || "");
  $("#segments").hidden = true;
  $("#order").hidden = true;
  notice("artist", "");
  skeleton($("#discography"), 10);
  if (kept) showSources(); else if (trackers) loadArtistSources();
  loadArtist(mbid);
}

function setArtistName(name) {
  $("#artist-name").textContent = name || t("artist.discography");
  document.title = name ? `${name} · Musaarr` : "Musaarr";
}

async function loadArtist(mbid) {
  state.artistController?.abort();
  const controller = new AbortController();
  state.artistController = controller;
  const list = $("#discography");
  list.setAttribute("aria-busy", "true");
  try {
    const data = await api(`/api/artists/${mbid}`, { signal: controller.signal });
    state.artist.items = data.results;
    state.artist.name = data.artist.name;
    state.artist.names = data.names || [];
    if (state.view === "artist") setArtistName(data.artist.name);
    $("#artist-name").textContent = data.artist.name;
    renderSegments();
    renderOrder();
    renderDiscography();
    renderAlbumHead();
    searchAlbum();
    if (!data.results.length) notice("artist", t("artist.noReleases"));
  } catch (err) {
    if (err.name === "AbortError") return;
    list.replaceChildren();
    const retry = () => {
      notice("artist", "");
      notice("album", "");
      skeleton(list, 10);
      loadArtist(mbid);
    };
    notice("artist", err.message, retry);
    if (state.view === "album") notice("album", err.message, retry);
  } finally {
    if (state.artistController === controller) list.setAttribute("aria-busy", "false");
  }
}

function renderSegments() {
  const counts = new Map(GROUPS.map((k) => [k, 0]));
  for (const item of state.artist.items) counts.set(group(item), counts.get(group(item)) + 1);
  const present = GROUPS.filter((k) => counts.get(k) > 0);
  if (!counts.get(state.artist.group)) state.artist.group = present[0];
  const box = $("#segments");
  box.hidden = present.length === 0;
  box.replaceChildren(...present.map((k) => {
    const b = el("button", "seg");
    b.type = "button";
    b.setAttribute("aria-pressed", String(k === state.artist.group));
    b.append(el("span", "", t(`group.${k}`)), el("span", "seg__n", num(counts.get(k))));
    b.addEventListener("click", () => {
      state.artist.group = k;
      for (const s of box.children) s.setAttribute("aria-pressed", String(s === b));
      renderDiscography();
    });
    return b;
  }));
}

// The order buttons show once ListenBrainz knows some of the albums.
function renderOrder() {
  const box = $("#order");
  box.hidden = !state.artist.items.some((item) => item.listens);
  for (const b of box.querySelectorAll("button")) b.setAttribute("aria-pressed", String(b.dataset.order === state.order));
}

function renderDiscography() {
  const byYear = (a, b) => (a.year || 9999) - (b.year || 9999); // oldest first, undated last
  const byListens = (a, b) => (b.listens || 0) - (a.listens || 0) || byYear(a, b);
  const listens = state.order === "listens" && !$("#order").hidden;
  const items = state.artist.items.filter((item) => group(item) === state.artist.group).sort(listens ? byListens : byYear);
  renderAlbums($("#discography"), items, { withArtist: false, artistPage: true });
}

/* ---------- The artist's releases on the trackers ---------- */

// loadArtistSources asks the trackers once for the open artist, under its name or under ask.
async function loadArtistSources(ask = "") {
  const a = state.artist;
  state.artistSourcesController?.abort();
  const controller = new AbortController();
  state.artistSourcesController = controller;
  a.sources = { status: "pending", ask };
  showSources();
  try {
    const data = await api(`/api/artists/${a.mbid}/sources${ask ? `?q=${encodeURIComponent(ask)}` : ""}`, { signal: controller.signal });
    a.sources = { status: "done", ask, data };
  } catch (err) {
    if (err.name === "AbortError") return;
    a.sources = { status: err.status === 503 ? "off" : "error", ask, error: err.message };
  }
  if (state.artist === a) showSources();
}

function showSources() {
  for (const p of document.querySelectorAll("#discography .card__quality")) fillQuality(p);
  renderTrackers();
  renderOthers();
}

function qualityName(q) {
  if (!q.format) return t("quality.unknown");
  return q.bits ? t("quality.formatBits", { format: q.format, bits: q.bits }) : q.format;
}

// fillQuality writes a card's best release; its line keeps its height while the trackers answer.
function fillQuality(p) {
  const s = state.artist?.sources;
  p.className = "card__quality";
  p.removeAttribute("aria-hidden");
  p.replaceChildren();
  p.hidden = !s || s.status === "off";
  if (p.hidden) return;
  if (s.status !== "done") { // the line under the artist's name says what is going on
    p.setAttribute("aria-hidden", "true");
    if (s.status === "pending") p.classList.add("is-pending");
    return;
  }
  const list = s.data.albums[p.dataset.mbid] || [];
  if (!list.length) {
    p.classList.add("is-none");
    p.textContent = t("sources.none");
    return;
  }
  const q = list[0].quality; // the server sends the best first
  p.append(el("span", q.lossless ? "chip chip--solid" : "chip", qualityName(q)),
    el("span", "card__count", tn("sources.releases", list.length)));
}

// Latin or Cyrillic names only: some indexers file Russian artists under their Cyrillic name; kana find nothing.
const USEFUL_NAME = /[a-zа-яё]/i;

function renderTrackers() {
  const a = state.artist;
  const s = a.sources;
  const box = $("#trackers");
  box.hidden = !s || s.status === "off";
  if (box.hidden) return;
  const text = $("#trackers-text");
  const actions = $("#trackers-actions");
  const hadFocus = actions.contains(document.activeElement);
  box.classList.toggle("is-error", s.status === "error");
  box.classList.toggle("is-pending", s.status === "pending");
  const buttons = [];
  if (s.status === "pending") {
    const who = s.ask || a.name;
    text.textContent = who ? t("trackers.searchingFor", { who }) : t("trackers.searching");
  } else if (s.status === "error") {
    text.textContent = s.error;
    const b = el("button", "btn btn--line btn--auto", t("action.retry"));
    b.type = "button";
    b.addEventListener("click", () => loadArtistSources(s.ask));
    buttons.push(b);
  } else {
    const { albums, others, names, query } = s.data;
    const lists = Object.values(albums);
    const n = lists.reduce((sum, l) => sum + l.length, 0) + others.length;
    const found = tn("trackers.found", n, { query });
    if (lists.length) text.textContent = tn("trackers.foundFor", lists.length, { found });
    else text.textContent = n ? t("trackers.foundNone", { found }) : t("trackers.nothing", { query });
    if (!lists.length) {
      for (const name of names) {
        if (!USEFUL_NAME.test(name) || name.toLowerCase() === query.toLowerCase()) continue;
        const b = el("button", "seg", t("trackers.searchUnder", { name }));
        b.type = "button";
        b.addEventListener("click", () => loadArtistSources(name)); // one tap, one search
        buttons.push(b);
      }
    }
  }
  actions.replaceChildren(...buttons);
  actions.hidden = buttons.length === 0;
  if (hadFocus) text.focus({ preventScroll: true }); // the tapped button is gone: stay next to it
}

function renderOthers() {
  const s = state.artist.sources;
  const others = s?.status === "done" ? s.data.others : [];
  $("#others-block").hidden = others.length === 0;
  $("#others-count").textContent = others.length ? num(others.length) : "";
  $("#others").replaceChildren(...others.map((item) => sourceRow(item, 3)));
}

/* ---------- Album page ---------- */

// albumItem is the open album: from the artist's discography when it is there (listens, live
// state), else from the album's own answer (a compilation, or a page opened from the library).
function albumItem() {
  return state.artist?.items?.find((item) => item.mbid === state.album)
    || (state.albumData?.album?.mbid === state.album ? state.albumData.album : undefined);
}

function openAlbum(artistMbid, albumMbid) {
  state.albumFromArtist = state.view === "artist" && state.artist?.mbid === artistMbid; // else Back opens the artist
  if (state.artist?.mbid !== artistMbid || !state.artist.items) startArtist(artistMbid, false);
  view("album");
  if (state.album !== albumMbid) {
    ALBUM_UI.controller?.abort();
    state.albumSearch = "";
    state.albumData = null;
    state.albumSources = null;
    $("#album-sources").replaceChildren();
    $("#album-status").textContent = "";
    $("#album-names").hidden = true;
    state.editionFilter = "*";
    $("#album-filter").hidden = true;
    $("#album-filter").replaceChildren();
    renderAlbumInfo();
  }
  state.album = albumMbid;
  notice("album", "");
  renderAlbumHead();
  if (state.albumData?.album?.mbid !== albumMbid) loadAlbum(albumMbid);
  searchAlbum();
  window.scrollTo(0, 0);
  $("#album-title").focus({ preventScroll: true });
}

function renderAlbumHead() {
  if (state.view !== "album") return;
  const a = state.artist;
  const item = albumItem();
  $("#album-back-name").textContent = state.fromLibrary ? t("nav.library") : state.fromShop ? t("nav.shop") : a.name || state.albumData?.album?.artist || t("artist.discography");
  if (!item) {
    const lost = a.items && state.albumData === false; // neither the discography nor the metadata server knows it
    $("#album-sleeve").replaceChildren();
    $("#album-title").textContent = lost ? t("album.notFound") : "";
    $("#album-sub").textContent = lost ? t("album.notInDiscography") : "";
    $("#album-action").replaceChildren();
    return;
  }
  setCover($("#album-sleeve"), item);
  $("#album-title").textContent = item.title;
  $("#album-sub").textContent = [subline(item), listensText(item.listens)].filter(Boolean).join(" · ");
  const r = state.requests.get(item.mbid);
  const shown = r ? { ...item, ...r } : item;
  const note = r && waitsForSource(r) ? el("p", "album-note", t("album.noteAttached"))
    : !shown.status ? el("p", "album-note", t("album.noteBest"))
    : "";
  $("#album-action").replaceChildren(cardAction(shown), note);
  document.title = `${item.title} · Musaarr`;
}

// searchAlbum asks the trackers for the open album itself, "Artist Album", once the page knows it:
// opening the page is the gesture, and this search finds more than the artist's.
function searchAlbum(name = "") {
  const item = albumItem();
  if (state.view !== "album" || !item || (!name && state.albumSearch === item.mbid)) return;
  const s = state.artist.sources;
  state.albumSearch = item.mbid;
  state.albumAsk = name || (s?.status === "done" && s.data.query) || item.artist; // the name the artist's page found it under
  runSources(`${state.albumAsk} ${item.title}`, ALBUM_UI, item.mbid, item.year);
}

// loadAlbum fetches the open album's tracks, editions and the version Plex holds.
async function loadAlbum(mbid) {
  state.albumController?.abort();
  const controller = new AbortController();
  state.albumController = controller;
  notice("album-info", "");
  $("#album-cols").hidden = false;
  $("#album-tracks-sub").textContent = "";
  trackSkeleton($("#album-tracks"));
  try {
    const data = await api(`/api/albums/${mbid}`, { signal: controller.signal });
    if (state.album !== mbid) return;
    state.albumData = data;
  } catch (err) {
    if (err.name === "AbortError" || state.album !== mbid) return;
    state.albumData = false;
    $("#album-cols").hidden = true;
    notice("album-info", t("album.infoUnavailable", { error: err.message }), () => loadAlbum(mbid));
  }
  renderAlbumHead();
  renderAlbumInfo();
  renderUpgrade(state.albumSources); // the trackers may have answered before Plex did
  renderEditionFilter(); // its buttons follow the editions' order, known only now
  searchAlbum(); // a page opened without its artist's discography knows the album only now
}

function trackSkeleton(box) {
  const list = el("ol", "tracklist skeleton");
  list.setAttribute("aria-hidden", "true");
  list.append(...Array.from({ length: 6 }, () => el("li", "track")));
  box.replaceChildren(list);
}

const fold = (t) => t.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().split(/[^\p{L}\p{N}]+/u).filter(Boolean).join(" ");

function duration(ms) {
  const s = Math.round(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

function totalTime(ms) {
  const min = Math.round(ms / 60000);
  return min >= 60 ? t("time.hm", { h: Math.floor(min / 60), m: String(min % 60).padStart(2, "0") }) : t("time.min", { n: min });
}

// tracklist draws tracks as one ordered list per disc; extra, when set, holds the folded titles the
// main edition lacks, marked as extra.
function tracklist(tracks, extra) {
  const discs = [...new Set(tracks.map((t) => t.disc))];
  const out = [];
  for (const d of discs) {
    if (discs.length > 1) out.push(el("h3", "tracklist__disc", t("tracks.disc", { n: d })));
    const list = el("ol", "tracklist");
    for (const tr of tracks.filter((x) => x.disc === d)) {
      const li = el("li", "track");
      li.append(el("span", "track__n", String(tr.n)), el("span", "track__title", tr.title));
      if (extra?.has(fold(tr.title))) li.append(el("span", "track__extra", t("tracks.extra")));
      li.append(el("span", "track__time", tr.ms ? duration(tr.ms) : ""));
      list.append(li);
    }
    out.push(list);
  }
  return out;
}

// clampTracks folds a long tracklist on a phone, where the sources wait under it.
function clampTracks(n) {
  const box = $("#album-tracks");
  box.classList.remove("is-clamped");
  if (n <= 8 || !matchMedia("(max-width: 899px)").matches) return [];
  box.classList.add("is-clamped");
  const b = el("button", "btn btn--line btn--auto tracks__more", t("tracks.showAll", { n: num(n) }));
  b.type = "button";
  b.setAttribute("aria-controls", "album-tracks");
  b.addEventListener("click", () => {
    box.classList.remove("is-clamped");
    b.remove();
    box.querySelector(".track:nth-child(7) .track__title")?.setAttribute("tabindex", "-1");
    box.querySelector(".track:nth-child(7) .track__title")?.focus({ preventScroll: true });
  });
  return [b];
}

function qualityOf(owned) {
  const rate = owned.rate ? t("quality.khz", { k: num(owned.rate / 1000, owned.rate % 1000 ? 1 : 0) }) : "";
  return [owned.codec ? owned.codec.toUpperCase() : "", owned.bits ? t("quality.bits", { bits: owned.bits }) : "", rate].filter(Boolean).join(" · ");
}

// renderAlbumInfo shows the version you own, the tracks, then the album's other editions.
function renderAlbumInfo() {
  const d = state.albumData;
  const owned = $("#album-owned");
  if (!d) {
    owned.hidden = true;
    $("#album-editions-block").hidden = true;
    if (d === null) $("#album-cols").hidden = true;
    return;
  }
  const eds = d.editions || [];
  const main = eds[0];
  const o = d.owned;
  owned.hidden = !o;
  if (o) {
    const ed = o.edition;
    const parts = [t("owned.inLibrary", { quality: qualityOf(o) || t("owned.qualityUnknown") })];
    if (ed?.exact) parts.push(t("owned.edition", { name: editionLabel(ed) })); // its name already counts the tracks
    else parts.push(tn("tracks.count", o.tracks.length), ...(ed ? [t("owned.near", { name: editionLabel(ed) })] : []));
    owned.textContent = parts.join(" · ");
  }
  const shown = o ? o.tracks : main?.tracks || [];
  $("#album-cols").hidden = !shown.length;
  $("#h-album-tracks").textContent = o ? t("tracks.yours") : t("tracks.title");
  const ms = shown.reduce((n, t) => n + (t.ms || 0), 0);
  const time = ms ? totalTime(ms) : "";
  $("#album-tracks-sub").textContent = o ? (time ? t("tracks.yourVersionTime", { time }) : t("tracks.yourVersion"))
    : main ? (time ? t("tracks.editionTime", { name: editionLabel(main), time }) : t("tracks.edition", { name: editionLabel(main) })) : "";
  $("#album-tracks").replaceChildren(...tracklist(shown), ...clampTracks(shown.length));
  const others = o ? eds : eds.slice(1);
  $("#album-editions-block").hidden = !others.length;
  $("#album-editions-sub").textContent = eds.length > 1
    ? t("editions.many", { n: num(eds.length) }) : t("editions.one");
  const base = new Set((main?.tracks || []).map((t) => fold(t.title)));
  $("#album-editions").replaceChildren(...others.map((e, i) => {
    const box = el("details", "edition");
    const head = el("summary", "edition__head");
    head.append(el("span", "edition__name", editionLabel(e)));
    const extraTitles = e === main ? [] : e.tracks.filter((t) => !base.has(fold(t.title))).map((t) => t.title);
    const extra = new Set(extraTitles.map(fold));
    const diff = !extraTitles.length ? "" : extraTitles.length <= 2 ? t("editions.extraList", { titles: extraTitles.join(", ") })
      : t("editions.extraCount", { n: num(extraTitles.length) });
    const n = [o && i === 0 ? t("editions.mostPressed") : "", tn("editions.pressings", e.pressings), diff].filter(Boolean).join(" · ");
    head.append(el("span", "edition__count", n));
    box.append(head, ...(e.label ? [el("p", "edition__label", e.label)] : []), ...tracklist(e.tracks, e === main ? null : extra));
    return box;
  }));
}

// renderAlbumNames offers the artist's other names when no release carries every word searched:
// trackers file Ye's albums under Kanye West.
function renderAlbumNames(items) {
  const box = $("#album-names");
  const ask = state.albumAsk.toLowerCase();
  const names = items && !items.some((it) => it.match)
    ? (state.artist?.names || []).filter((n) => USEFUL_NAME.test(n) && n.toLowerCase() !== ask) : [];
  box.replaceChildren(...names.map((name) => {
    const b = el("button", "seg", t("trackers.searchUnder", { name }));
    b.type = "button";
    b.addEventListener("click", () => {
      searchAlbum(name); // one tap, one search
      $("#h-album-sources").focus({ preventScroll: true }); // the tapped button is gone
    });
    return b;
  }));
  box.hidden = names.length === 0;
}

/* ---------- Navigation ---------- */

// leaveArtist stops what the artist's pages still wait for; they ask again when opened.
function leaveArtist() {
  state.artistController?.abort();
  state.albumController?.abort();
  state.artistSourcesController?.abort();
  ALBUM_UI.controller?.abort();
  state.albumSearch = "";
  if (state.artist?.sources?.status === "pending") state.artist.sources = null;
}

function toSearch() {
  leaveArtist();
  const tab = state.returnTab; // back where "Pick a source" or an album was tapped
  state.returnTab = null;
  state.fromLibrary = false;
  const fromShop = state.fromShop;
  state.fromShop = false;
  const moved = state.view !== "search";
  if (moved) {
    view("search");
    document.title = "Musaarr";
  }
  if (tab) show(tab); // that tab puts back its own scroll
  else if (moved) requestAnimationFrame(() => {
    window.scrollTo(0, state.searchScroll);
    if (fromShop) shopReturn(); // back on the crate the album came out of
  });
}

function openSourcesView() {
  leaveArtist();
  view("sources");
  document.title = `${t("trackers.title")} · Musaarr`;
  window.scrollTo(0, 0);
  $("#sources-title").focus({ preventScroll: true });
}

function back(parent = "") {
  if (state.navigated) { history.back(); return; }
  history.replaceState(null, "", parent || location.pathname + location.search); // opened from a link or a reload
  route();
}

function route() {
  const h = location.hash;
  if (h === "#disquaire") { // the shop in 3D, over the page
    if (state.tab !== "search") show("search");
    openShop3D();
    return;
  }
  closeShop3D();
  const shelf =/^#bibliotheque\/(\d{1,12})$/.exec(h);
  if (shelf) { // a link to a Plex album: open its page once Musaarr knows it
    if (state.tab !== "library") show("library");
    openLibraryAlbum(shelf[1], true);
    return;
  }
  const album = /^#artiste\/([0-9a-f-]{36})\/album\/([0-9a-f-]{36})$/i.exec(h);
  const artist = /^#artiste\/([0-9a-f-]{36})$/i.exec(h);
  if ((album || artist || h === "#trackers") && state.tab !== "search") {
    state.returnTab = state.tab;
    show("search");
  }
  if (album) openAlbum(album[1].toLowerCase(), album[2].toLowerCase());
  else if (artist) openArtist(artist[1].toLowerCase());
  else if (h === "#trackers") openSourcesView();
  else toSearch();
}

/* ---------- Requests ---------- */

// After a pick, retrying also has Lidarr follow the album again.
function retryButton(item) {
  const b = el("button", "btn btn--line btn--auto", t("request.retry"));
  b.type = "button";
  b.dataset.key = `${item.mbid}:retry`;
  b.addEventListener("click", async () => {
    b.disabled = true;
    try {
      const saved = await api(`/api/requests/${item.mbid}/retry`, { method: "POST", body: "{}" });
      state.requests.set(saved.mbid, saved);
      renderRequests();
      announce(t("request.retried", { title: item.title }));
    } catch (err) {
      b.disabled = false;
      toast(err.message);
    }
  });
  return b;
}

// Remove asks twice on the button itself: no dialog for a step a new request undoes.
function removeButton(item) {
  const b = el("button", "btn btn--quiet btn--auto", t("request.remove"));
  b.type = "button";
  b.dataset.key = `${item.mbid}:remove`;
  const disarm = () => {
    clearTimeout(b.timer);
    b.classList.remove("is-armed");
    b.textContent = t("request.remove");
  };
  b.addEventListener("click", async () => {
    if (!b.classList.contains("is-armed")) {
      b.classList.add("is-armed");
      b.textContent = t("request.confirmRemove");
      announce(t("request.tapAgain", { title: item.title }));
      b.timer = setTimeout(disarm, 5000);
      return;
    }
    clearTimeout(b.timer);
    b.disabled = true;
    try {
      await api(`/api/requests/${item.mbid}`, { method: "DELETE" });
      setRequests([...state.requests.values()].filter((r) => r.mbid !== item.mbid));
      $("#library-title").focus();
      toast(t(item.status === "in_plex" ? "request.removedPlex" : "request.removed", { title: item.title }));
      announce(t("request.removedShort", { title: item.title }));
    } catch (err) {
      b.disabled = false;
      disarm();
      toast(err.message);
    }
  });
  return b;
}

function rowActions(item) {
  const actions = [];
  if (waitsForSource(item)) { // the album's page lists its releases; an old request without its artist gets the free search
    const b = el("a", "btn btn--line btn--auto", t("request.pickSource"));
    b.href = item.artistMbid ? `#artiste/${item.artistMbid}/album/${item.mbid}` : "#trackers";
    b.dataset.key = `${item.mbid}:source`;
    if (!item.artistMbid) b.addEventListener("click", () => { $("#sq").value = `${item.artist} ${item.title}`; });
    actions.push(b, retryButton(item));
  }
  if (item.status === "in_plex") {
    if (item.plexUrl) {
      const a = el("a", "btn btn--blue btn--auto", t("action.openInPlex"));
      a.href = item.plexUrl;
      a.target = "_blank";
      a.rel = "noopener";
      a.dataset.key = `${item.mbid}:plex`;
      actions.push(a);
    }
    const p = el("a", "btn btn--line btn--auto", t("action.openPlexamp"));
    p.href = "plexamp://";
    p.dataset.key = `${item.mbid}:plexamp`;
    actions.push(p);
  }
  if (item.removable) actions.push(removeButton(item));
  return actions;
}

function summary(items) {
  const count = (test) => items.filter((r) => test(r.status)).length;
  return [
    ["summary.inPlex", lib.albums.length],
    ["summary.active", count((s) => ACTIVE.has(s))],
    ["summary.waiting", count((s) => s === "waiting")],
    ["summary.check", count((s) => s === "check" || s === "failed")],
  ].filter(([, n]) => n > 0).map(([key, n]) => tn(key, n)).join(" · ");
}

// row draws one request; the compact one (search home) keeps only the cover, the names and the band.
function row(item, compact = false) {
  const node = $("#tpl-row").content.firstElementChild.cloneNode(true);
  if (compact) node.classList.add("row--compact");
  setCover(node.querySelector(".sleeve"), item);
  node.querySelector(".row__title").textContent = item.title;
  node.querySelector(".row__sub").textContent = compact ? item.artist : subline(item);
  let from;
  if (!compact) {
    const p = item.status === "downloading" ? item.progress : 100;
    const prev = state.shown.get(item.mbid);
    state.shown.set(item.mbid, { status: item.status, p });
    if (prev && prev.status === item.status) from = prev.p;
  }
  const b = band(item, { big: !compact, from, fresh: state.fresh.has(item.mbid) }); // an arrival inks in again
  b.classList.add("row__band");
  node.querySelector(".row__band").replaceWith(b);
  if (compact) return node;
  if (EXPLAINED.has(item.status) && !(item.status === "check" && item.picked)) { // a stuck pick has its own note
    const explain = node.querySelector(".row__explain");
    explain.hidden = false;
    explain.textContent = t(`explain.${item.status}`);
  }
  if (item.picked) {
    const src = el("p", "row__explain", t("request.source", { name: item.picked, from: item.pickedFrom }));
    node.querySelector(".row__note").before(src);
  }
  if (item.note) {
    const note = node.querySelector(".row__note");
    note.hidden = false;
    note.textContent = item.noteCode ? t(`note.${item.noteCode}`, item.noteParams) : item.note;
  }
  node.querySelector(".row__actions").append(...rowActions(item));
  return node;
}

// On the way: what needs you first, then what moves, then what waits for a source; newest first
// within each. What reached Plex leaves the list: the shelf under it shows it.
const ORDER = { check: 0, failed: 0, waiting: 2 };

function renderRequests() {
  const items = [...state.requests.values()];
  const focusKey = document.activeElement?.dataset?.key;
  const open = items.filter((r) => r.status !== "in_plex").sort((a, b) => (ORDER[a.status] ?? 1) - (ORDER[b.status] ?? 1));
  $("#library-summary").textContent = summary(items);
  $("#enroute-block").hidden = open.length === 0;
  $("#requests").replaceChildren(...open.map((item) => row(item)));
  if (focusKey) document.querySelector(`[data-key="${CSS.escape(focusKey)}"]`)?.focus();
}

function renderRecent() {
  const items = [...state.requests.values()].slice(0, 3);
  $("#recent-block").hidden = items.length === 0;
  $("#recent").replaceChildren(...items.map((item) => row(item, true)));
}

function updateBadge() {
  const n = [...state.requests.values()].filter((r) => ACTIVE.has(r.status)).length;
  const badge = $("#active-count");
  badge.hidden = n === 0;
  badge.textContent = n;
  badge.setAttribute("aria-label", tn("summary.active", n));
}

// setRequests draws a list of requests; quiet is for the copy kept from the last visit.
function setRequests(list, quiet = false) {
  const key = JSON.stringify(list);
  if (key === state.listKey) return; // nothing moved: keep the DOM, the focus and VoiceOver's place
  state.listKey = key;
  const before = state.requests;
  state.requests = new Map(list.map((r) => [r.mbid, r]));
  if (!quiet) {
    for (const r of list) {
      const old = before.get(r.mbid);
      if (old && old.status !== r.status) announce(t("fmt.colon", { a: r.title, b: statusText(r) }));
      if (old && old.status !== "in_plex" && r.status === "in_plex") state.fresh.add(r.mbid);
    }
    if (state.fresh.size) {
      loadLibrary(); // the shelf shows it at once
      stageSay("arrived", state.fresh.size); // since the last visit: a green glint
    }
    try { localStorage.setItem("musaarr.requests", key); } catch { /* private mode or full */ }
  }
  updateBadge();
  renderRecent();
  if (state.tab === "library") renderRequests();
  state.fresh.clear();
}

// The last list seen shows at once, before the network answers, and stays readable offline.
function restoreRequests() {
  try {
    const list = JSON.parse(localStorage.getItem("musaarr.requests") || "[]");
    if (Array.isArray(list)) setRequests(list, true);
  } catch { /* nothing usable kept */ }
}

async function loadRequests() {
  try {
    const data = await api("/api/requests");
    $("#offline").hidden = true;
    setRequests(data.requests);
  } catch (err) {
    const away = Boolean(err.away);
    $("#offline").hidden = !away;
    if (state.tab === "library" && !away) toast(err.message);
  }
}

function schedule() {
  clearInterval(state.timer);
  if (document.visibilityState === "visible") state.timer = setInterval(loadRequests, 10000);
}

/* ---------- Library ---------- */

// The shelf: every album Plex holds, sorted and filtered here. Cards are drawn LIB_PAGE at a time
// as the page nears its end, so a library of thousands opens at once on a phone.
const LIB_PAGE = 60;
const collator = new Intl.Collator(i18n.lang, { sensitivity: "base", numeric: true });
const lib = {
  albums: [], byKey: new Map(), plex: "", text: "", sorted: {}, loaded: false, drawn: false, loading: false, timer: 0,
  view: "albums", sort: "added", decade: "", genre: "", kind: "", unplayed: false, q: "", artist: "",
  list: [], shown: 0, scroll: 0, artistScroll: 0, opened: "", opening: "",
};
const sortName = (name) => name.replace(/^the\s+/i, "");
const SORTS = {
  added: (a, b) => b.added - a.added,
  played: (a, b) => b.played - a.played || b.added - a.added,
  artist: (a, b) => collator.compare(a.sortArtist, b.sortArtist) || (a.year || 9999) - (b.year || 9999) || collator.compare(a.title, b.title),
  year: (a, b) => b.year - a.year || collator.compare(a.sortArtist, b.sortArtist) || collator.compare(a.title, b.title),
  title: (a, b) => collator.compare(a.title, b.title) || collator.compare(a.sortArtist, b.sortArtist),
};
const albumsText = (n) => tn("albums.count", n);
const dayFmt = new Intl.DateTimeFormat(i18n.lang, { day: "numeric", month: "short" });
const dayYearFmt = new Intl.DateTimeFormat(i18n.lang, { day: "numeric", month: "short", year: "numeric" });

function when(seconds) {
  const d = new Date(seconds * 1000);
  return (d.getFullYear() === new Date().getFullYear() ? dayFmt : dayYearFmt).format(d);
}

function setLibrary(data) {
  lib.albums = (data.albums || []).map((a) => ({
    ...a, year: a.year || 0, added: a.added || 0, played: a.played || 0, plays: a.plays || 0, genres: a.genres || [],
    sortArtist: sortName(a.artist), search: fold(`${a.title} ${a.artist} ${(a.genres || []).join(" ")}`),
  }));
  lib.byKey = new Map(lib.albums.map((a) => [a.key, a]));
  lib.plex = data.plex || "";
  lib.sorted = {};
  lib.loaded = true;
  if (lib.artist && !lib.albums.some((a) => a.artist === lib.artist)) lib.artist = "";
  fillFilters();
  renderLibrary(true);
  $("#library-summary").textContent = summary([...state.requests.values()]);
}

// The last library seen shows at once, before the network answers, and stays readable offline.
function restoreLibrary() {
  try {
    const text = localStorage.getItem("musaarr.library");
    if (text) {
      lib.text = text;
      setLibrary(JSON.parse(text));
    }
  } catch { /* nothing usable kept */ }
}

async function loadLibrary() {
  if (lib.loading) return;
  lib.loading = true;
  if (!lib.loaded) skeleton($("#library"), 10);
  try {
    const data = await api("/api/library");
    notice("library", "");
    const text = JSON.stringify(data);
    if (text !== lib.text) {
      lib.text = text;
      setLibrary(data);
      try { localStorage.setItem("musaarr.library", text); } catch { /* full: the tab works without it */ }
    }
  } catch (err) {
    if (err.away && lib.loaded) return; // the offline banner says it; the last shelf stays
    notice("library", lib.loaded ? t("library.stale", { error: err.message }) : err.message, loadLibrary);
    if (!lib.loaded) $("#library").replaceChildren();
  } finally {
    lib.loading = false;
  }
}

function fillFilters() {
  const decades = new Map();
  const genres = new Map();
  for (const a of lib.albums) {
    if (a.year) {
      const d = Math.floor(a.year / 10) * 10;
      decades.set(d, (decades.get(d) || 0) + 1);
    }
    for (const g of a.genres) genres.set(g, (genres.get(g) || 0) + 1);
  }
  const fill = (select, first, entries, label) => {
    select.replaceChildren(new Option(first, ""), ...entries.map(([v]) => new Option(label(v), String(v))));
  };
  fill($("#lib-decade"), t("library.decades"), [...decades].sort((a, b) => b[0] - a[0]), (d) => t("library.decade", { d }));
  fill($("#lib-genre"), t("library.genres"), [...genres].sort((a, b) => collator.compare(a[0], b[0])), (g) => g);
  const kinds = new Set(lib.albums.map((a) => a.kind || "album"));
  const kindSelect = $("#lib-kind");
  kindSelect.replaceChildren(new Option(t("library.kinds"), ""), ...Object.keys(KIND_NAMES).filter((k) => kinds.has(k)).map((k) => new Option(KIND_NAMES[k], k)));
  if (lib.kind && !kinds.has(lib.kind)) lib.kind = "";
  kindSelect.value = lib.kind;
  kindSelect.parentElement.hidden = kinds.size < 2;
  if (lib.decade && !decades.has(Number(lib.decade))) lib.decade = "";
  if (lib.genre && !genres.has(lib.genre)) lib.genre = "";
  $("#lib-decade").value = lib.decade;
  $("#lib-genre").value = lib.genre;
  $("#lib-decade").parentElement.hidden = decades.size < 2;
  $("#lib-genre").parentElement.hidden = genres.size === 0;
}

const libFiltered = () => Boolean(lib.unplayed || lib.decade || lib.genre || lib.kind || lib.q.trim());

function libMatch(a, q) {
  return (!lib.unplayed || (!a.played && !a.plays))
    && (!lib.kind || (a.kind || "album") === lib.kind)
    && (!lib.decade || Math.floor(a.year / 10) * 10 === Number(lib.decade))
    && (!lib.genre || a.genres.includes(lib.genre))
    && (!q || a.search.includes(q));
}

// renderLibrary redraws the shelf for the current choices; keep holds as many cards as were shown
// (a refresh from Plex must not lose the place the page was scrolled to).
function renderLibrary(keep = false) {
  lib.drawn = true;
  const has = lib.albums.length > 0;
  $("#shelf-tools").hidden = !has;
  $("#lib-form").hidden = !has;
  $("#library-empty").hidden = !lib.loaded || has;
  lib.sorted[lib.sort] ||= [...lib.albums].sort(SORTS[lib.sort]);
  const q = fold(lib.q);
  const pool = lib.sorted[lib.sort].filter((a) => libMatch(a, q));
  lib.list = lib.artist ? pool.filter((a) => a.artist === lib.artist) : pool;
  const artists = new Map();
  for (const a of pool) {
    const ar = artists.get(a.artist);
    if (ar) {
      ar.n++;
      ar.cover ||= a.cover;
    } else {
      artists.set(a.artist, { name: a.artist, sort: a.sortArtist, n: 1, cover: a.cover });
    }
  }
  $("#lib-n-albums").textContent = num(pool.length);
  $("#lib-n-artists").textContent = num(artists.size);
  for (const b of $("#lib-views").querySelectorAll(".seg")) b.setAttribute("aria-pressed", String(b.dataset.view === lib.view));
  $("#lib-unplayed").setAttribute("aria-pressed", String(lib.unplayed));
  $("#lib-decade").parentElement.classList.toggle("is-set", Boolean(lib.decade));
  $("#lib-genre").parentElement.classList.toggle("is-set", Boolean(lib.genre));
  $("#lib-kind").parentElement.classList.toggle("is-set", Boolean(lib.kind));
  $("#lib-artist").hidden = !lib.artist;
  $("#lib-artist-name").textContent = lib.artist;
  const roster = lib.view === "artists" && !lib.artist;
  libCount(pool.length, artists.size, roster);
  $("#library").hidden = roster;
  $("#library-artists").hidden = !roster;
  renderRoom();
  if (!has) {
    $("#library").replaceChildren();
    return;
  }
  if (roster) renderRoster([...artists.values()].sort((x, y) => collator.compare(x.sort, y.sort)));
  else drawCards(keep ? Math.max(lib.shown, LIB_PAGE) : LIB_PAGE);
}

function libCount(albums, artists, roster) {
  const filtered = libFiltered();
  let text = "";
  if (roster) {
    if (!artists) text = t("library.noArtist");
    else if (filtered) text = t("library.artistsFor", { artists: tn("artists.count", artists), albums: albumsText(albums) });
  } else if (!lib.list.length) {
    text = t("library.noAlbum");
  } else if (lib.artist) {
    text = albumsText(lib.list.length);
  } else if (filtered) {
    text = t("library.of", { albums: albumsText(lib.list.length), total: num(lib.albums.length) });
  }
  $("#lib-count").textContent = text;
  $("#lib-clear").hidden = !filtered;
}

function drawCards(n) {
  lib.shown = 0;
  $("#library").replaceChildren();
  moreCards(shelfWatch ? n : lib.list.length); // no observer to page with: draw them all
}

function moreCards(n = LIB_PAGE) {
  const next = lib.list.slice(lib.shown, lib.shown + n);
  lib.shown += next.length;
  $("#library").append(...next.map(libCard));
}

// The sentinel under the grid draws the next cards well before the thumb gets there; observing it
// again after each page fires once more if it is still in reach.
const shelfWatch = "IntersectionObserver" in window ? new IntersectionObserver((entries) => {
  if (!entries.some((e) => e.isIntersecting) || lib.shown >= lib.list.length || !$("#library-artists").hidden) return;
  moreCards();
  requestAnimationFrame(() => {
    shelfWatch.unobserve($("#lib-more"));
    shelfWatch.observe($("#lib-more"));
  });
}, { rootMargin: "0px 0px 1200px 0px" }) : null;

function libMeta(a) {
  let context = a.genres[0] || "";
  if (lib.sort === "added" && a.added) context = t("library.addedOn", { date: when(a.added) });
  if (lib.sort === "played") context = a.played ? t("library.playedOn", { date: when(a.played) }) : t("library.neverPlayed");
  return [a.year || "", context].filter(Boolean).join(" · ");
}

function libCard(a) {
  const node = $("#tpl-card").content.firstElementChild.cloneNode(true);
  node.dataset.key = a.key;
  const sleeve = node.querySelector(".sleeve");
  setCover(sleeve, a);
  const href = `#bibliotheque/${a.key}`;
  const link = el("a", "card__link", a.title);
  link.href = href;
  link.dataset.key = `lib:${a.key}`;
  node.querySelector(".card__title").replaceChildren(link);
  const cover = el("a", "card__cover"); // a second way in for the finger; the title link is the one read out
  cover.href = href;
  cover.tabIndex = -1;
  cover.setAttribute("aria-hidden", "true");
  sleeve.replaceWith(cover);
  cover.append(sleeve);
  node.querySelector(".card__sub").textContent = a.artist;
  node.querySelector(".card__meta").textContent = libMeta(a);
  node.querySelector(".card__action").remove();
  return node;
}

// renderRoster lists the artists of the albums the choices keep, each under one of their covers.
function renderRoster(artists) {
  $("#library-artists").replaceChildren(...artists.map((ar) => {
    const li = el("li");
    const b = el("button", "shelf__who");
    b.type = "button";
    b.dataset.artist = ar.name;
    const sleeve = el("span", "sleeve sleeve--row shelf__who-sleeve");
    sleeve.setAttribute("aria-hidden", "true");
    setCover(sleeve, ar);
    const text = el("span", "shelf__who-text");
    text.append(el("span", "shelf__who-name", ar.name), el("span", "shelf__who-n", albumsText(ar.n)));
    b.append(sleeve, text);
    li.append(b);
    return li;
  }));
}

function openShelfArtist(name) {
  lib.artistScroll = window.scrollY;
  lib.artist = name;
  renderLibrary();
  $("#lib-artist-name").focus({ preventScroll: true });
  $("#shelf-block").scrollIntoView({ block: "start" });
}

function closeShelfArtist() {
  const name = lib.artist;
  lib.artist = "";
  renderLibrary();
  requestAnimationFrame(() => window.scrollTo(0, lib.artistScroll));
  $(`#library-artists [data-artist="${CSS.escape(name)}"]`)?.focus({ preventScroll: true });
}

function saveShelf() {
  try { localStorage.setItem("musaarr.shelf", JSON.stringify({ sort: lib.sort, view: lib.view })); } catch { /* private mode */ }
}

// openLibraryAlbum finds the MusicBrainz album behind a Plex album and opens its page; replace is for
// a #bibliotheque/<key> link, which leaves no step in the history.
async function openLibraryAlbum(key, replace = false, from = null) {
  if (lib.opening === key) return;
  lib.opening = key; // a later tap wins: this one then leaves the page where it is
  const a = lib.byKey.get(key);
  const card = from || document.querySelector(`#library [data-key="${CSS.escape(key)}"]`);
  card?.classList.add("is-opening");
  card?.setAttribute("aria-busy", "true");
  announce(a ? t("library.openingAlbum", { title: a.title }) : t("library.opening"));
  try {
    const ref = await api(`/api/library/${key}/album`);
    if (lib.opening !== key || state.tab !== "library") return;
    lib.opened = key;
    state.fromLibrary = true;
    if (crate.tab === "library") suspendCrate();
    const hash = `#artiste/${ref.artistMbid}/album/${ref.mbid}`;
    if (replace) {
      history.replaceState(null, "", hash);
      route();
    } else {
      location.hash = hash;
    }
  } catch (err) {
    if (replace) history.replaceState(null, "", location.pathname + location.search);
    if (lib.opening !== key) return;
    if (err.status === 404 && card && a) unmatched(card.closest(".dig") ? $("#crate-actions") : card, a, err.message);
    else toast(err.message);
  } finally {
    if (lib.opening === key) lib.opening = "";
    card?.classList.remove("is-opening", "is-lifting");
    card?.removeAttribute("aria-busy");
  }
}

// unmatched tells, on the card itself, that Musaarr has no page for this album, and offers Plex's.
function unmatched(card, a, message) {
  card.querySelector(".card__miss")?.remove();
  const box = el("div", "card__miss");
  box.append(el("p", "card__miss-text", t("library.notInMb")));
  card.append(box);
  announce(t("fmt.colon", { a: a.title, b: message }));
  if (!lib.plex) return;
  const go = el("a", "btn btn--line", t("action.openInPlex"));
  go.href = lib.plex + encodeURIComponent(`/library/metadata/${a.key}`);
  go.target = "_blank";
  go.rel = "noopener";
  go.setAttribute("aria-label", t("library.openInPlexLabel", { title: a.title }));
  box.append(go);
  go.focus();
}

// returnToShelf puts the tab back where it was, on the card that was opened.
function returnToShelf() {
  if (crate.resume?.tab === "library") {
    resumeCrate();
    lib.opened = "";
    return;
  }
  requestAnimationFrame(() => {
    window.scrollTo(0, lib.scroll);
    if (!lib.opened) return;
    const key = CSS.escape(lib.opened);
    const link = document.querySelector(`#racks [data-key="lib:${key}"]`) || document.querySelector(`#library [data-key="lib:${key}"]`);
    link?.focus({ preventScroll: true });
    lib.opened = "";
  });
}

function initShelf() {
  try {
    const kept = JSON.parse(localStorage.getItem("musaarr.shelf") || "{}");
    if (SORTS[kept.sort]) lib.sort = kept.sort;
    if (kept.view === "artists") lib.view = "artists";
  } catch { /* private mode */ }
  $("#lib-sort").value = lib.sort;
  $("#lq").addEventListener("input", () => {
    clearTimeout(lib.timer);
    lib.timer = setTimeout(() => {
      lib.q = $("#lq").value;
      renderLibrary();
    }, 120);
  });
  $("#lib-form").addEventListener("submit", (e) => {
    e.preventDefault();
    clearTimeout(lib.timer);
    lib.q = $("#lq").value;
    renderLibrary();
    $("#lq").blur();
  });
  for (const b of $("#lib-views").querySelectorAll(".seg")) {
    b.addEventListener("click", () => {
      lib.view = b.dataset.view;
      lib.artist = "";
      saveShelf();
      renderLibrary();
    });
  }
  $("#lib-sort").addEventListener("change", (e) => {
    lib.sort = e.target.value;
    saveShelf();
    renderLibrary();
  });
  $("#lib-decade").addEventListener("change", (e) => { lib.decade = e.target.value; renderLibrary(); });
  $("#lib-genre").addEventListener("change", (e) => { lib.genre = e.target.value; renderLibrary(); });
  $("#lib-kind").addEventListener("change", (e) => { lib.kind = e.target.value; renderLibrary(); });
  $("#lib-unplayed").addEventListener("click", () => { lib.unplayed = !lib.unplayed; renderLibrary(); });
  $("#lib-clear").addEventListener("click", () => {
    Object.assign(lib, { q: "", decade: "", genre: "", kind: "", unplayed: false });
    $("#lq").value = "";
    $("#lib-decade").value = "";
    $("#lib-genre").value = "";
    $("#lib-kind").value = "";
    renderLibrary();
    $("#lq").focus();
  });
  $("#lib-artist-back").addEventListener("click", closeShelfArtist);
  $("#library-artists").addEventListener("click", (e) => {
    const b = e.target.closest("button[data-artist]");
    if (b) openShelfArtist(b.dataset.artist);
  });
  $("#library").addEventListener("click", (e) => {
    const a = e.target.closest('a[href^="#bibliotheque/"]');
    if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return; // a new tab follows the link itself
    e.preventDefault();
    openLibraryAlbum(a.getAttribute("href").slice("#bibliotheque/".length));
  });
  shelfWatch?.observe($("#lib-more"));
}

/* ---------- The room ---------- */

// The room: shelves of covers standing on planks, each with the object you chose, in the wood
// and on the wall you chose. Kept on the server, so every device shows the same room.
const RACK_MAX = 20;
// names reads an id's words when asked (the catalogue arrives after this script runs); keys maps id -> key.
const names = (keys) => new Proxy(keys, { get: (o, id) => (Object.hasOwn(o, id) ? t(o[id]) : undefined) });
const keyed = (prefix, ids) => Object.fromEntries(ids.map((id) => [id, `${prefix}.${id || "none"}`]));
const KINDS = keyed("kind", ["album", "single", "live", "compilation", "soundtrack", "remix"]);
const KIND_NAMES = names(KINDS);
const SHELF_NAMES = names({ ...keyed("shelf", ["deck", "vitrine", "top", "recent", "added", "never", "rediscover"]), ...KINDS });
const SHELF_DECO = { deck: "enceinte", top: "plante", added: "lampe", rediscover: "casque", album: "platine", compilation: "cactus" };
// Ids are kept in rooms already saved: they stay French whatever the reader's language.
const choices = (prefix, ids) => Object.entries(keyed(prefix, ids)); // [id, key]
const FINISHES = choices("finish", ["chene", "noyer", "noir", "blanc", "acier"]);
const WALLS = choices("wall", ["uni", "pois", "lattes", "brique"]);
const DECOS = choices("deco", ["", "platine", "plante", "lampe", "enceinte", "casque", "cactus"]);
const VITRINE_MAX = 24;
const room = { finish: "noyer", wall: "uni", shelves: [], vitrine: [], open: false, timer: 0, saved: "" };

const roomDefaults = () => Object.keys(SHELF_NAMES).map((id) => ({ id, off: false, deco: SHELF_DECO[id] || "" }));
const roomData = () => ({ finish: room.finish, wall: room.wall, shelves: room.shelves, vitrine: room.vitrine });
const known = (list, v) => list.some(([k]) => k === v);

// setRoom takes a room as the NAS or this device kept it; what this build does not know is left out,
// and a shelf this build adds shows up at the end.
function setRoom(r = {}) {
  room.finish = known(FINISHES, r.finish) ? r.finish : "noyer";
  room.wall = known(WALLS, r.wall) ? r.wall : "uni";
  const seen = new Set();
  room.shelves = [];
  for (const s of Array.isArray(r.shelves) ? r.shelves : []) {
    if (!SHELF_NAMES[s?.id] || seen.has(s.id)) continue;
    seen.add(s.id);
    room.shelves.push({ id: s.id, off: Boolean(s.off), deco: known(DECOS, s.deco) ? s.deco : "" });
  }
  for (const s of roomDefaults()) if (!seen.has(s.id)) room.shelves.push(s);
  room.vitrine = (Array.isArray(r.vitrine) ? r.vitrine : []).filter((k) => /^\d{1,12}$/.test(k)).slice(0, VITRINE_MAX);
  $("#wall").dataset.finish = room.finish;
  $("#wall").dataset.wall = room.wall;
  if (room.open) fillDecor();
  if (lib.drawn) renderRoom();
}

async function loadRoom() {
  try {
    const kept = localStorage.getItem("musaarr.room");
    if (kept) setRoom(JSON.parse(kept));
  } catch { /* nothing usable kept */ }
  try {
    const data = await api("/api/room");
    room.saved = JSON.stringify(data);
    if (!data.finish && !data.wall && !data.shelves && !data.vitrine && localStorage.getItem("musaarr.room")) return; // never saved on the NAS yet
    setRoom(data);
    keepRoom();
  } catch { /* the room of this device stays */ }
}

function keepRoom() {
  try { localStorage.setItem("musaarr.room", JSON.stringify(roomData())); } catch { /* private mode */ }
}

// saveRoom sends the room to the NAS once the changes stop; until then the page already shows them.
function saveRoom() {
  keepRoom();
  clearTimeout(room.timer);
  const status = $("#decor-status");
  status.classList.remove("is-error");
  room.timer = setTimeout(async () => {
    try {
      const saved = await api("/api/room", { method: "PUT", body: JSON.stringify(roomData()) });
      room.saved = JSON.stringify(saved);
      status.textContent = t("room.saved");
    } catch (err) {
      status.classList.add("is-error");
      status.textContent = t("room.notSaved", { error: err.message });
    }
  }, 600);
}

const DAY = 86400;
const today = () => Math.floor(Date.now() / 864e5);

// shuffled draws the same order all day for a given seed: "Never played" does not reshuffle on each visit.
function shuffled(list, seed) {
  let t = seed >>> 0;
  const rand = () => { // mulberry32
    t = (t + 0x6D2B79F5) >>> 0;
    let x = Math.imul(t ^ (t >>> 15), 1 | t);
    x = (x + Math.imul(x ^ (x >>> 7), 61 | x)) ^ x;
    return ((x ^ (x >>> 14)) >>> 0) / 4294967296;
  };
  const out = [...list];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

const sortedBy = (key) => (lib.sorted[key] ||= [...lib.albums].sort(SORTS[key]));

// rackList is every album of a shelf, in its order; the shelf shows the first RACK_MAX, its crate all.
function rackList(id) {
  if (id === "vitrine") return room.vitrine.map((k) => lib.byKey.get(k)).filter(Boolean);
  const cached = lib.sorted[`rack:${id}`];
  if (cached) return cached;
  let list;
  if (id === "deck") list = sortedBy("played").filter((a) => a.played).slice(0, 1);
  else if (id === "top") list = lib.albums.filter((a) => a.plays > 0).sort((x, y) => y.plays - x.plays || y.played - x.played);
  else if (id === "recent") list = sortedBy("played").filter((a) => a.played);
  else if (id === "added") list = sortedBy("added");
  else if (id === "never") list = shuffled(lib.albums.filter((a) => !a.plays && !a.played), today());
  else if (id === "rediscover") {
    const yearAgo = Date.now() / 1000 - 365 * DAY;
    list = shuffled(lib.albums.filter((a) => a.played && a.played < yearAgo), today() + 7);
  } else list = sortedBy("added").filter((a) => (a.kind || "album") === id);
  lib.sorted[`rack:${id}`] = list;
  return list;
}

const listens = (n) => tn("plays.count", n);

function rackMeta(id, a) {
  if (id === "top") return listens(a.plays);
  if (id === "recent" || id === "rediscover" || id === "deck") return a.played ? t("library.playedOn", { date: when(a.played) }) : "";
  if (id === "added" && a.added) return t("library.addedOn", { date: when(a.added) });
  return [a.year || "", a.genres[0] || ""].filter(Boolean).join(" · ");
}

// renderRoom draws the shelves; a search or a filter hides them, so only the results show.
function renderRoom() {
  const show = lib.loaded && lib.albums.length > 0 && !libFiltered() && !lib.artist;
  $("#room").hidden = !show;
  if (!show) return;
  const box = $("#racks");
  const scrolls = new Map([...box.querySelectorAll(".rack")].map((r) => [r.dataset.rack, r.querySelector(".rack__scroll").scrollLeft]));
  const focused = box.contains(document.activeElement) ? document.activeElement.dataset.key || document.activeElement.dataset.dig : "";
  const kinds = new Set(lib.albums.map((a) => a.kind || "album"));
  const racks = [];
  for (const s of room.shelves) {
    if (s.off || (KIND_NAMES[s.id] && (kinds.size < 2 || !kinds.has(s.id)))) continue;
    const list = rackList(s.id);
    if (!list.length && s.id !== "vitrine") continue;
    if (!list.length && !room.open) continue; // the empty display shows while decorating, as a hint
    racks.push(rack(s, list));
  }
  box.replaceChildren(...racks);
  for (const r of box.querySelectorAll(".rack")) r.querySelector(".rack__scroll").scrollLeft = scrolls.get(r.dataset.rack) || 0;
  if (focused) box.querySelector(`[data-key="${CSS.escape(focused)}"], [data-dig="${CSS.escape(focused)}"]`)?.focus({ preventScroll: true });
}

function rack(s, list) {
  const id = `rack-${s.id}`;
  const sec = el("section", `rack rack--${s.id}`);
  sec.dataset.rack = s.id;
  sec.setAttribute("aria-labelledby", id);
  const head = el("div", "rack__head");
  const title = el("h3", "rack__title", SHELF_NAMES[s.id]);
  title.id = id;
  head.append(title);
  if (s.id !== "deck" && list.length) head.append(el("span", "rack__n", albumsText(list.length)));
  const actions = el("div", "rack__actions");
  if (list.length > 1) {
    const dig = el("button", "more", t("room.dig"));
    dig.type = "button";
    dig.dataset.dig = s.id;
    dig.setAttribute("aria-label", t("room.digLabel", { name: SHELF_NAMES[s.id] }));
    actions.append(dig);
  }
  if (KIND_NAMES[s.id] && list.length > RACK_MAX) {
    const all = el("button", "more", t("action.seeAll"));
    all.type = "button";
    all.dataset.all = s.id;
    all.setAttribute("aria-label", t("room.seeAllLabel", { albums: albumsText(list.length), name: SHELF_NAMES[s.id] }));
    actions.append(all);
  }
  const scroll = el("div", "rack__scroll");
  actions.append(sideArrows(scroll, SHELF_NAMES[s.id]));
  slideWithMouse(scroll);
  head.append(actions);
  const items = el("ul", "rack__items");
  items.setAttribute("aria-labelledby", id);
  if (s.deco) items.append(decoItem(s.deco));
  if (!list.length) items.append(el("li", "rack__empty", t("room.vitrineEmpty")));
  list.slice(0, RACK_MAX).forEach((a, n) => items.append(s.id === "deck" ? deckItem(a) : recItem(s.id, a, s.id === "top" ? n + 1 : 0)));
  const first = items.querySelector(".rec__link");
  if (first) first.tabIndex = 0;
  scroll.append(items);
  sec.append(head, scroll);
  return sec;
}

function decoItem(name) {
  const li = el("li", "rack__deco");
  li.setAttribute("aria-hidden", "true");
  li.append($(`#tpl-deco-${name}`).content.firstElementChild.cloneNode(true));
  return li;
}

function recLink(a) {
  const link = el("a", "rec__link");
  link.href = `#bibliotheque/${a.key}`;
  link.dataset.key = `lib:${a.key}`;
  link.tabIndex = -1;
  return link;
}

function recItem(id, a, rank) {
  const li = el("li", "rec");
  const link = recLink(a);
  const art = el("span", "rec__art");
  const disc = el("span", "disc rec__disc");
  const sleeve = el("span", "sleeve rec__sleeve");
  setCover(sleeve, a);
  art.append(disc, sleeve);
  link.append(art);
  if (rank) link.append(el("span", "rec__rank", String(rank)));
  link.append(el("span", "rec__title", a.title), el("span", "rec__sub", a.artist));
  const meta = rackMeta(id, a);
  if (meta) link.append(el("span", "rec__meta", meta));
  li.append(link);
  return li;
}

function deckItem(a) {
  const li = el("li", "rec rec--deck");
  const link = recLink(a);
  const art = el("span", "deck__art");
  const disc = el("span", "deck__disc");
  disc.append(el("span", "disc deck__spin"));
  const sleeve = el("span", "sleeve rec__sleeve");
  setCover(sleeve, a);
  art.append(disc, sleeve);
  link.append(art, el("span", "rec__title", a.title), el("span", "rec__sub", a.artist));
  const meta = rackMeta("deck", a);
  if (meta) link.append(el("span", "rec__meta", meta));
  li.append(link);
  return li;
}

/* Decorate */

function swatches(box, name, options, value, attr) {
  box.replaceChildren(...options.map(([v, label]) => {
    const l = el("label", "swatch");
    const input = el("input", "swatch__input");
    input.type = "radio";
    input.name = name;
    input.value = v;
    input.checked = v === value;
    const chip = el("span", "swatch__chip");
    chip.dataset[attr] = v;
    chip.setAttribute("aria-hidden", "true");
    l.append(input, chip, el("span", "swatch__name", t(label)));
    return l;
  }));
}

const chevron = (d) => {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("viewBox", "0 0 24 24");
  svg.setAttribute("aria-hidden", "true");
  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  path.setAttribute("d", d);
  svg.append(path);
  return svg;
};

// sideArrows: ‹ › for a row that scrolls sideways, the one way a mouse wheel cannot move it; greyed at
// its ends, gone when the row fits.
function sideArrows(row, name) {
  const box = el("div", "side-arrows");
  const arrow = (dir) => {
    const b = el("button", "side-arrow");
    b.type = "button";
    b.setAttribute("aria-label", t(dir < 0 ? "rail.prev" : "rail.next", { name }));
    b.append(chevron(dir < 0 ? "M15 5l-7 7 7 7" : "M9 5l7 7-7 7"));
    b.addEventListener("click", () => row.scrollBy({ left: dir * row.clientWidth * 0.85, behavior: reduceMotion.matches ? "auto" : "smooth" }));
    return b;
  };
  const prev = arrow(-1), next = arrow(1);
  const sync = () => {
    prev.disabled = row.scrollLeft < 4;
    next.disabled = row.scrollLeft + row.clientWidth >= row.scrollWidth - 4;
    box.hidden = prev.disabled && next.disabled;
  };
  row.addEventListener("scroll", sync, { passive: true });
  if ("ResizeObserver" in window) new ResizeObserver(sync).observe(row);
  else requestAnimationFrame(sync);
  box.append(prev, next);
  return box;
}

// slideWithMouse: a mouse dragged sideways slides the row, and the click that ends the drag opens nothing.
function slideWithMouse(row) {
  let d = null;
  row.addEventListener("pointerdown", (e) => {
    row.dataset.slid = "";
    if (e.pointerType === "mouse" && e.button === 0) d = { id: e.pointerId, x: e.clientX, left: row.scrollLeft, moved: false };
  });
  row.addEventListener("pointermove", (e) => {
    if (!d || d.id !== e.pointerId) return;
    if (!(e.buttons & 1)) { end(); return; } // released outside the row
    const dx = e.clientX - d.x;
    if (!d.moved && Math.abs(dx) > 8) {
      d.moved = true;
      row.setPointerCapture(e.pointerId);
      row.classList.add("is-sliding");
    }
    if (d.moved) row.scrollLeft = d.left - dx;
  });
  const end = () => {
    row.dataset.slid = d?.moved ? "1" : "";
    row.classList.remove("is-sliding");
    d = null;
  };
  row.addEventListener("pointerup", end);
  row.addEventListener("pointercancel", end);
  row.addEventListener("click", (e) => {
    if (!row.dataset.slid) return;
    e.preventDefault();
    e.stopPropagation();
    row.dataset.slid = "";
  }, true);
  row.addEventListener("dragstart", (e) => e.preventDefault());
}

function fillDecor() {
  swatches($("#decor-finish"), "finish", FINISHES, room.finish, "finish");
  swatches($("#decor-wall"), "wall", WALLS, room.wall, "wall");
  const n = room.shelves.length;
  $("#decor-shelves").replaceChildren(...room.shelves.map((s, i) => {
    const name = SHELF_NAMES[s.id];
    const li = el("li", "decor__shelf");
    li.dataset.shelf = s.id;
    const check = el("label", "decor__check");
    const box = el("input");
    box.type = "checkbox";
    box.checked = !s.off;
    box.dataset.act = "show";
    check.append(box, el("span", "", name));
    const pick = el("span", "pick");
    const select = el("select");
    select.dataset.act = "deco";
    select.setAttribute("aria-label", t("decor.objectOn", { name }));
    select.append(...DECOS.map(([v, label]) => new Option(t(label), v, false, v === s.deco)));
    pick.append(select);
    const move = el("span", "decor__move");
    const up = el("button", "seg seg--small");
    up.type = "button";
    up.dataset.act = "up";
    up.disabled = i === 0;
    up.setAttribute("aria-label", t("decor.up", { name }));
    up.append(chevron("M6 15l6-6 6 6"));
    const down = el("button", "seg seg--small");
    down.type = "button";
    down.dataset.act = "down";
    down.disabled = i === n - 1;
    down.setAttribute("aria-label", t("decor.down", { name }));
    down.append(chevron("M6 9l6 6 6-6"));
    move.append(up, down);
    li.append(check, pick, move);
    return li;
  }));
}

function toggleDecor(open) {
  room.open = open;
  $("#decor").hidden = !open;
  $("#room-decorate").setAttribute("aria-expanded", String(open));
  $("#decor-status").textContent = "";
  if (open) fillDecor();
  renderRoom();
}

function initRoom() {
  room.shelves = roomDefaults();
  $("#room-decorate").addEventListener("click", () => {
    toggleDecor(!room.open);
    if (room.open) $("#decor-finish input:checked")?.focus();
  });
  $("#decor-done").addEventListener("click", () => {
    toggleDecor(false);
    $("#room-decorate").focus();
  });
  $("#decor").addEventListener("change", (e) => {
    const input = e.target;
    if (input.name === "finish") room.finish = input.value;
    else if (input.name === "wall") room.wall = input.value;
    else {
      const s = room.shelves.find((x) => x.id === input.closest("[data-shelf]")?.dataset.shelf);
      if (!s) return;
      if (input.dataset.act === "show") s.off = !input.checked;
      if (input.dataset.act === "deco") s.deco = input.value;
    }
    $("#wall").dataset.finish = room.finish;
    $("#wall").dataset.wall = room.wall;
    renderRoom();
    saveRoom();
  });
  $("#decor-shelves").addEventListener("click", (e) => {
    const b = e.target.closest("button[data-act]");
    if (!b) return;
    const i = room.shelves.findIndex((x) => x.id === b.closest("[data-shelf]").dataset.shelf);
    const j = b.dataset.act === "up" ? i - 1 : i + 1;
    if (i < 0 || j < 0 || j >= room.shelves.length) return;
    [room.shelves[i], room.shelves[j]] = [room.shelves[j], room.shelves[i]];
    const id = room.shelves[j].id;
    fillDecor();
    const again = $(`#decor-shelves [data-shelf="${id}"] [data-act="${b.dataset.act}"]`);
    (again && !again.disabled ? again : $(`#decor-shelves [data-shelf="${id}"] [data-act="${b.dataset.act === "up" ? "down" : "up"}"]`))?.focus();
    announce(t(b.dataset.act === "up" ? "decor.movedUp" : "decor.movedDown", { name: SHELF_NAMES[id], pos: j + 1, n: room.shelves.length }));
    renderRoom();
    saveRoom();
  });
  $("#decor-reset").addEventListener("click", () => {
    setRoom({ vitrine: room.vitrine });
    saveRoom();
    $("#decor-status").textContent = t("decor.resetDone");
  });
  $("#room-dig").addEventListener("click", (e) => openShelfCrate("all", e.currentTarget));
  const racks = $("#racks");
  racks.addEventListener("click", (e) => {
    const dig = e.target.closest("button[data-dig]");
    if (dig) { openShelfCrate(dig.dataset.dig, dig); return; }
    const all = e.target.closest("button[data-all]");
    if (all) {
      lib.kind = all.dataset.all;
      $("#lib-kind").value = lib.kind;
      renderLibrary();
      $("#shelf-block").scrollIntoView({ block: "start" });
      $("#lib-kind").focus({ preventScroll: true });
      return;
    }
    const a = e.target.closest('a[href^="#bibliotheque/"]');
    if (!a || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
    e.preventDefault();
    openLibraryAlbum(a.getAttribute("href").slice("#bibliotheque/".length), false, a.closest(".rec"));
  });
  // One stop per shelf for the Tab key; the arrows move along it.
  racks.addEventListener("keydown", (e) => {
    const link = e.target.closest(".rec__link");
    if (!link) return;
    const links = [...link.closest(".rack__items").querySelectorAll(".rec__link")];
    const i = links.indexOf(link);
    const to = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: links.length - 1 }[e.key];
    if (to === undefined) return;
    e.preventDefault();
    links[Math.max(0, Math.min(links.length - 1, to))].focus();
  });
  racks.addEventListener("focusin", (e) => {
    const link = e.target.closest(".rec__link");
    if (!link) return;
    for (const l of link.closest(".rack__items").querySelectorAll(".rec__link")) l.tabIndex = l === link ? 0 : -1;
  });
  loadRoom();
}

/* ---------- The crate ---------- */

// The crate: the records stand one behind the other; pulling the front one toward you (finger,
// wheel, arrows) lays it down and shows the next. Only the records around the front one exist
// in the page, so a crate of thousands stays light.
const CRATE_BEHIND = 5;
const crate = { opts: null, list: [], i: 0, tab: "", nodes: new Map(), drag: null, wheel: 0, wheelAt: 0, sayTimer: 0, resume: null, from: null, scroll: 0 };
const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)");

// openCrate shows the crate in opts.tab: { tab, title, sub, label, back, list, draw(item, node),
// info(item) -> { name, meta }, actions(item, box), open(item, node), finish }.
function openCrate(opts, at = 0, from = null) {
  if (crate.tab && crate.tab !== opts.tab) $(`#panel-${crate.tab}`).classList.remove("is-digging");
  crate.i = at;
  crate.tab = opts.tab;
  crate.resume = null;
  if (from) {
    crate.from = from;
    crate.scroll = window.scrollY;
  }
  const box = $("#crate");
  const panel = $(`#panel-${opts.tab}`);
  panel.append(box);
  panel.classList.add("is-digging");
  box.hidden = false;
  $("#crate-back-name").textContent = opts.back;
  fillCrate(opts, crate.i);
  $("#crate-tools").replaceChildren(...(opts.tools ? opts.tools() : []));
  window.scrollTo(0, 0);
  $("#crate-stage").focus({ preventScroll: true });
}

// fillCrate puts another list in the open crate (a filter changed), from record at.
function fillCrate(opts, at = 0) {
  crate.opts = opts;
  crate.list = opts.list;
  crate.i = Math.max(0, Math.min(at, crate.list.length - 1));
  const n = opts.count ?? crate.list.length;
  $("#crate").dataset.finish = opts.finish || "chene";
  $("#crate-title").textContent = opts.title;
  $("#crate-sub").textContent = opts.sub || "";
  $("#crate-label").textContent = opts.label || opts.title;
  $("#crate-stage").setAttribute("aria-label", t("crate.label", { title: opts.title, records: tn("records.count", n) }));
  for (const node of crate.nodes.values()) node.remove();
  crate.nodes.clear();
  layoutCrate(false);
}

function closeCrate() {
  if (!crate.tab) return;
  $(`#panel-${crate.tab}`).classList.remove("is-digging");
  $("#crate").hidden = true;
  crate.tab = "";
  crate.resume = null;
  clearTimeout(crate.sayTimer);
  const from = crate.from;
  crate.from = null;
  requestAnimationFrame(() => {
    window.scrollTo(0, crate.scroll);
    const d = from?.dataset || {};
    const again = d.dig ? $(`#racks [data-dig="${CSS.escape(d.dig)}"]`) : null;
    (from?.isConnected ? from : again)?.focus({ preventScroll: true });
  });
}

// suspendCrate hides the crate while an album page from it is open; resumeCrate brings it back
// on the same record.
function suspendCrate() {
  crate.resume = { opts: crate.opts, i: crate.i, tab: crate.tab, from: crate.from, scroll: crate.scroll };
  $(`#panel-${crate.tab}`).classList.remove("is-digging");
  $("#crate").hidden = true;
  crate.tab = "";
}

function resumeCrate() {
  const r = crate.resume;
  if (!r) return false;
  openCrate(r.opts, r.i);
  crate.from = r.from;
  crate.scroll = r.scroll;
  return true;
}

function digNode(item, k) {
  const node = el("div", "dig");
  node.dataset.k = String(k);
  crate.opts.draw(item, node);
  return node;
}

function layoutCrate(animate = true) {
  const recs = $("#crate-records");
  recs.classList.toggle("is-still", !animate || reduceMotion.matches);
  const lo = crate.i - 1;
  const hi = crate.i + CRATE_BEHIND;
  for (const [k, node] of crate.nodes) {
    if (k < lo || k > hi) {
      node.remove();
      crate.nodes.delete(k);
    }
  }
  for (let k = Math.max(0, lo); k <= Math.min(hi, crate.list.length - 1); k++) {
    const d = k - crate.i;
    let node = crate.nodes.get(k);
    if (!node) {
      node = digNode(crate.list[k], k);
      node.style.setProperty("--d", String(d > 0 ? d + 1 : d));
      if (d > 0) node.style.opacity = "0";
      if (d < 0) node.classList.add("is-gone");
      recs.append(node);
      crate.nodes.set(k, node);
      if (animate) void node.offsetWidth; // start from one slot further back, then move in
    }
    node.style.setProperty("--d", String(d));
    node.style.setProperty("--pull", "0");
    node.style.opacity = "";
    node.classList.remove("is-dragging", "is-lifting");
    node.classList.toggle("is-front", d === 0);
    node.classList.toggle("is-gone", d < 0);
  }
  drawCrateNow();
}

function drawCrateNow() {
  const item = crate.list[crate.i];
  const n = crate.list.length;
  const bottom = crate.i === n - 1 && n > 1 ? ` · ${t("crate.bottom")}` : "";
  $("#crate-pos").textContent = n ? t("crate.pos", { i: num(crate.i + 1), n: num(n) }) + bottom : t("crate.empty");
  const info = item ? crate.opts.info(item) : { name: "", meta: "" };
  $("#crate-name").textContent = info.name;
  $("#crate-meta").textContent = info.meta;
  const box = $("#crate-actions");
  const had = box.contains(document.activeElement) ? document.activeElement.dataset.act : "";
  box.replaceChildren();
  if (item) crate.opts.actions(item, box);
  if (had) box.querySelector(`[data-act="${had}"]`)?.focus();
  $("#crate-prev").disabled = crate.i === 0;
  $("#crate-next").disabled = crate.i >= n - 1;
}

// crateGo moves the front to record i; the position and title are read out once the flicking stops.
function crateGo(i, animate = true) {
  const to = Math.max(0, Math.min(crate.list.length - 1, i));
  if (to === crate.i) {
    layoutCrate(animate);
    return;
  }
  const far = Math.abs(to - crate.i) > CRATE_BEHIND;
  crate.i = to;
  if (far) {
    for (const n of crate.nodes.values()) n.remove();
    crate.nodes.clear();
  }
  layoutCrate(animate && !far);
  clearTimeout(crate.sayTimer);
  crate.sayTimer = setTimeout(() => {
    const item = crate.list[crate.i];
    if (item && crate.tab) announce(t("fmt.colon", { a: $("#crate-pos").textContent, b: crate.opts.info(item).name }));
  }, 450);
}

function crateOpen() {
  const item = crate.list[crate.i];
  const node = crate.nodes.get(crate.i);
  if (!item || !crate.opts.open) return;
  node?.classList.add("is-lifting");
  setTimeout(() => crate.opts.open(item, node), reduceMotion.matches ? 0 : 160);
}

function crateDrag(dy) {
  const front = crate.nodes.get(crate.i);
  const prev = crate.nodes.get(crate.i - 1);
  const p = Math.max(0, Math.min(1, dy / 220));
  const q = Math.max(0, Math.min(1, -dy / 220));
  front?.classList.add("is-dragging");
  front?.style.setProperty("--pull", String(p));
  if (prev) {
    prev.classList.add("is-dragging");
    prev.style.setProperty("--pull", String(q));
  }
}

function initCrate() {
  const stage = $("#crate-stage");
  $("#crate-back").addEventListener("click", closeCrate);
  $("#crate-prev").addEventListener("click", () => crateGo(crate.i - 1));
  $("#crate-next").addEventListener("click", () => crateGo(crate.i + 1));
  $("#crate-shuffle").addEventListener("click", () => crateGo(Math.floor(Math.random() * crate.list.length), false));
  $("#crate").addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      e.preventDefault();
      closeCrate();
    }
  });
  stage.addEventListener("keydown", (e) => {
    const step = { ArrowDown: 1, ArrowRight: 1, PageDown: 5, ArrowUp: -1, ArrowLeft: -1, PageUp: -5 }[e.key];
    if (step) {
      e.preventDefault();
      crateGo(crate.i + step);
    } else if (e.key === "Home" || e.key === "End") {
      e.preventDefault();
      crateGo(e.key === "Home" ? 0 : crate.list.length - 1, false);
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      crateOpen();
    }
  });
  stage.addEventListener("dragstart", (e) => e.preventDefault()); // a cover dragged as an image would cancel the gesture
  stage.addEventListener("pointerdown", (e) => {
    if (e.button !== 0) return;
    crate.drag = { x: e.clientX, y: e.clientY, t: performance.now(), dy: 0, moved: false, k: e.target.closest(".dig")?.dataset.k };
    stage.setPointerCapture(e.pointerId);
    stage.classList.add("is-grabbing");
  });
  stage.addEventListener("pointermove", (e) => {
    const d = crate.drag;
    if (!d) return;
    d.dy = e.clientY - d.y;
    if (Math.abs(d.dy) > 6 || Math.abs(e.clientX - d.x) > 6) d.moved = true;
    if (d.moved && !reduceMotion.matches) crateDrag(d.dy);
  });
  const release = (e) => {
    const d = crate.drag;
    crate.drag = null;
    stage.classList.remove("is-grabbing");
    if (!d) return;
    if (e.type === "pointercancel") { layoutCrate(); return; }
    const v = d.dy / Math.max(1, performance.now() - d.t);
    if (!d.moved) {
      const k = Number(d.k);
      if (d.k === undefined) return;
      if (k === crate.i) crateOpen();
      else crateGo(k); // a record behind, by the top that shows
      return;
    }
    if (d.dy > 70 || v > 0.5) crateGo(crate.i + 1);
    else if (d.dy < -70 || v < -0.5) crateGo(crate.i - 1);
    else layoutCrate();
  };
  stage.addEventListener("pointerup", release);
  stage.addEventListener("pointercancel", release);
  stage.addEventListener("wheel", (e) => {
    e.preventDefault();
    const now = performance.now();
    if (now - crate.wheelAt > 160) crate.wheel = 0;
    crate.wheelAt = now;
    crate.wheel += e.deltaY;
    if (Math.abs(crate.wheel) >= 60) {
      crateGo(crate.i + Math.sign(crate.wheel));
      crate.wheel = 0;
    }
  }, { passive: false });
}

/* The Library's crates */

function libCrateDraw(a, node) {
  const sleeve = el("span", "sleeve");
  setCover(sleeve, a);
  const img = sleeve.querySelector("img");
  if (img) img.loading = "eager";
  node.append(sleeve);
}

function libCrateInfo(a) {
  const listened = a.plays ? listens(a.plays) : a.played ? "" : t("library.neverPlayed");
  return { name: a.title, meta: [a.artist, a.year || "", KIND_NAMES[a.kind] && a.kind ? KIND_NAMES[a.kind] : "", listened].filter(Boolean).join(" · ") };
}

function libCrateActions(a, box) {
  const open = el("a", "btn btn--line", t("crate.openAlbum"));
  open.href = `#bibliotheque/${a.key}`;
  open.dataset.act = "open";
  open.addEventListener("click", (e) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return;
    e.preventDefault();
    crateOpen();
  });
  const pinned = room.vitrine.includes(a.key);
  const pin = el("button", "seg seg--small", t(pinned ? "crate.inVitrine" : "crate.pin"));
  pin.type = "button";
  pin.dataset.act = "pin";
  pin.setAttribute("aria-pressed", String(pinned));
  pin.addEventListener("click", () => {
    if (room.vitrine.includes(a.key)) room.vitrine = room.vitrine.filter((k) => k !== a.key);
    else if (room.vitrine.length >= VITRINE_MAX) { toast(t("crate.vitrineFull", { n: VITRINE_MAX })); return; }
    else room.vitrine = [a.key, ...room.vitrine];
    saveRoom();
    renderRoom();
    drawCrateNow();
    announce(t(room.vitrine.includes(a.key) ? "crate.pinned" : "crate.unpinned", { title: a.title }));
  });
  box.append(open, pin);
}

function openShelfCrate(id, from) {
  const all = id === "all";
  let list = all ? shuffled(lib.albums, Date.now()) : rackList(id);
  if (KIND_NAMES[id] || id === "never" || id === "rediscover") list = shuffled(list, Date.now()); // a bin is dug in no order
  const title = all ? t("crate.wholeLibrary") : SHELF_NAMES[id];
  openCrate({
    tab: "library", title, label: all ? t("nav.library") : title, back: t("nav.library"), finish: room.finish,
    sub: all ? t("crate.shuffled", { albums: albumsText(list.length) }) : albumsText(list.length),
    list, draw: libCrateDraw, info: libCrateInfo, actions: libCrateActions,
    open: (a, node) => openLibraryAlbum(a.key, false, node),
  }, 0, from);
}

/* ---------- The 3D stage ---------- */

function stageSay(name, detail) {
  document.dispatchEvent(new CustomEvent(`musaarr:${name}`, { detail }));
}

// fly sends a record from the button just pressed to the Library tab, which pulses.
function fly(from) {
  const tab = $("#tab-library");
  const to = tab.querySelector("svg").getBoundingClientRect();
  if (reduceMotion.matches || !from.width || !to.width) return;
  const disc = el("span", "disc fly");
  disc.setAttribute("aria-hidden", "true");
  document.body.append(disc);
  const x0 = from.left + from.width / 2 - 18, y0 = from.top + from.height / 2 - 18;
  const x1 = to.left + to.width / 2 - 18, y1 = to.top + to.height / 2 - 18;
  const cx = (x0 + x1) / 2, cy = Math.min(y0, y1) - 140; // an arc over the page
  const frames = Array.from({ length: 13 }, (_, i) => {
    const t = i / 12, u = 1 - t;
    const x = u * u * x0 + 2 * u * t * cx + t * t * x1, y = u * u * y0 + 2 * u * t * cy + t * t * y1;
    return { transform: `translate(${x}px, ${y}px) rotate(${t * 600}deg) scale(${1 - t * 0.5})`, opacity: t < 0.9 ? 1 : (1 - t) * 10 };
  });
  disc.animate(frames, { duration: 760, easing: "cubic-bezier(.77, 0, .175, 1)" }).finished
    .then(() => {
      tab.classList.remove("is-pulsing");
      void tab.offsetWidth;
      tab.classList.add("is-pulsing");
    })
    .catch(() => { /* cancelled */ })
    .finally(() => disc.remove());
}

// loadStage brings the 3D record after the first paint; on a first visit the opening plays, over
// the ground, so the app does not flash before it. Without WebGL 2 the import fails: no 3D.
function loadStage() {
  if (reduceMotion.matches || navigator.connection?.saveData || !window.WebGL2RenderingContext) return;
  const veil = $("#opening");
  let opened = true;
  try { opened = localStorage.getItem("musaarr.opened") === "1"; } catch { /* private mode */ }
  const lift = () => { if (!veil.dataset.playing) veil.hidden = true; };
  if (!opened) {
    veil.hidden = false;
    $("#opening-skip").addEventListener("click", lift);
    setTimeout(lift, 4000); // the 3D is slow to come: the app first
  }
  const go = () => import("./stage.js").catch(() => { veil.hidden = true; });
  requestAnimationFrame(() => setTimeout(go, opened ? 400 : 0));
}

/* ---------- Sources ---------- */

const LAYOUTS = names({ tracks: "layout.tracks", image: "layout.image" });

function size(bytes) {
  if (!bytes) return ""; // some trackers give no size
  const mo = bytes / 2 ** 20;
  return mo < 1000 ? t("size.mb", { n: num(mo) }) : t("size.gb", { n: num(mo / 1024, 1) });
}

function age(iso, now = Date.now()) {
  const at = Date.parse(iso);
  if (!at || new Date(at).getFullYear() < 1990) return ""; // Prowlarr sends year 1 when the indexer gives no date
  const days = Math.floor((now - at) / 864e5);
  if (days < 1) return t("age.today");
  if (days < 45) return t("age.days", { n: days });
  if (days < 365) return t("age.months", { n: Math.round(days / 30.44) });
  return tn("age.years", Math.floor(days / 365.25));
}

// editionLabel names an edition from its parts in the reader's language: « Japan · 2006 · CD · 12 tracks ».
// An edition without parts (an older server) keeps its name.
function editionLabel(e) {
  if (!e.count) return e.name;
  const country = (e.country || "").split(", ").find(Boolean);
  return [country && countryName(country), e.year, e.format && formatName(e.format), e.disambiguation, tn("tracks.count", e.count)]
    .filter(Boolean).join(" · ");
}

// MusicBrainz names a country by its code, or by its English name through Lidarr's metadata server.
const COUNTRY_CODES = { "United States": "US", "United Kingdom": "GB", Japan: "JP", Germany: "DE", France: "FR", Canada: "CA",
  Europe: "XE", "[Worldwide]": "XW", Australia: "AU", Netherlands: "NL", Italy: "IT", Spain: "ES", Sweden: "SE", Brazil: "BR",
  Russia: "RU", "Russian Federation": "RU", "South Korea": "KR", "Korea, Republic of": "KR", Switzerland: "CH", Austria: "AT", Belgium: "BE" };
let regions;

function countryName(c) {
  const code = COUNTRY_CODES[c] || c;
  if (code === "XE") return t("country.europe");
  if (code === "XW") return t("country.world");
  if (!/^[A-Z]{2}$/.test(code)) return c;
  try {
    regions ||= new Intl.DisplayNames([i18n.lang], { type: "region" });
    return regions.of(code);
  } catch { return c; } // an old browser, or a code no region has
}

let formats, formatRe;

// formatName words a MusicBrainz format the reader's way: « 2 Vinyle 12" » in French.
function formatName(f) {
  if (!formats) { // the first match wins at each place: the longer names come first
    formats = new Map([["Digital Media", t("format.digital")], ['12" Vinyl', t("format.vinyl12")], ['10" Vinyl', t("format.vinyl10")],
      ['7" Vinyl', t("format.vinyl7")], ["Vinyl", t("format.vinyl")], ["Hybrid SACD", t("format.hybridSacd")],
      ["Enhanced CD", t("format.enhancedCd")], ["CD-R", t("format.cdr")]]);
    formatRe = new RegExp([...formats.keys()].join("|"), "g");
  }
  return f.replace(formatRe, (m) => formats.get(m));
}

// wordFor words a code the server sends (a quality flag, an edition hint): key prefix.code, with
// "code:year" for one with a year; a name it has no key for (SHM-CD, MQA) stays as it is.
function wordFor(prefix, s) {
  const [code, year] = s.split(":");
  const key = year ? `${prefix}.${code}.year` : `${prefix}.${code}`;
  return i18n.has(key) ? t(key, { year }) : s;
}

function seeders(n) {
  return n === 0 ? t("seeders.none") : tn("seeders.count", n);
}

function qualityChips(q) {
  const chips = [];
  if (q.format) chips.push([q.format, q.lossless ? "chip chip--solid" : "chip"]);
  if (q.bits) chips.push([t("quality.bits", { bits: q.bits })]);
  if (q.khz) chips.push([t("quality.khz", { k: num(parseFloat(q.khz), 1) })]);
  if (LAYOUTS[q.layout]) chips.push([LAYOUTS[q.layout]]);
  for (const f of q.flags) chips.push([wordFor("flag", f)]);
  if (!q.lossless && q.format) chips.push([t("quality.lossy")]);
  if (!q.format) chips.push([t("quality.unknown")]);
  return chips.map(([text, cls = "chip"]) => el("li", cls, text));
}

// sourceRow draws a release; album, when set, is the album page it sits on.
function sourceRow(item, level = 2, album = "") {
  const node = $("#tpl-source").content.firstElementChild.cloneNode(true);
  if (level !== 2) node.querySelector(".source__title").replaceWith(el(`h${level}`, "source__title"));
  node.classList.toggle("source--dead", item.seeders === 0);
  node.querySelector(".source__title").textContent = item.title;
  node.querySelector(".source__meta").textContent = [item.indexer, size(item.size), seeders(item.seeders), age(item.published)].filter(Boolean).join(" · ");
  node.querySelector(".source__chips").append(...qualityChips(item.quality));
  node.querySelector(".source__warn").hidden = !["ISO", "DSD"].includes(item.quality.format);
  node.dataset.place = item.place || "";
  setEditionLine(node, item);
  const hint = node.querySelector(".source__hint");
  hint.hidden = !item.hints?.length;
  hint.textContent = item.hints?.length ? t("source.hints", { hints: item.hints.map((h) => wordFor("hint", h)).join(", ") }) : "";
  const b = el("button", "btn btn--blue btn--auto", t("source.download"));
  b.type = "button";
  b.setAttribute("aria-label", t("source.downloadLabel", { title: item.title }));
  b.addEventListener("click", () => grab(item, b, album));
  const look = el("button", "btn btn--line btn--auto", t("source.peek"));
  look.type = "button";
  look.setAttribute("aria-label", t("source.peekLabel", { title: item.title }));
  look.addEventListener("click", () => peek(item, look, node.querySelector(".source__files"), album));
  node.querySelector(".source__action").append(b, look);
  return node;
}

// setEditionLine says, on an album's page, which edition a release is: guessed from its name, read
// from its .torrent (read: true), or still to confirm; or that it is another record of the artist.
function setEditionLine(node, item, read = false) {
  const line = node.querySelector(".source__edition");
  const e = item.edition;
  let text = "";
  if (item.place === "album" && e) {
    text = t(!read ? "source.editionLikely" : e.exact ? "source.editionRead" : "source.editionNearest", { name: editionLabel(e) });
  } else if (item.place === "album") text = t("source.editionUnknown");
  else if (item.place === "other") text = t("source.otherRecord", { name: item.other });
  line.hidden = !text;
  line.textContent = text;
  line.classList.toggle("source__edition--open", item.place === "album" && !e);
  node.dataset.edition = e?.key || "";
  node.dataset.editionName = e ? editionLabel(e) : "";
}

// peek reads a release's file list from its .torrent (nothing is downloaded) and says which edition
// of the album it is, when it sits on the album's page.
async function peek(item, button, box, album) {
  const focused = document.activeElement === button; // read before disabling it, which blurs it
  button.disabled = true;
  button.textContent = t("source.reading");
  box.classList.remove("is-error");
  try {
    const res = await api("/api/sources/files", { method: "POST", body: JSON.stringify({ token: item.token, album }) });
    const said = el("p", "files__verdict", verdict(res));
    said.tabIndex = -1;
    box.replaceChildren(said, ...filesDetails(res));
    button.remove();
    const row = box.closest(".source");
    if (row?.dataset.place === "album" && res.edition && !res.edition.byCount) { // the files beat the name
      item.edition = res.edition;
      setEditionLine(row, item, true);
      if (state.editionFilter !== "*" && state.editionFilter !== row.dataset.edition) state.editionFilter = "*"; // keep the row it read in sight
      renderEditionFilter();
    }
    if (focused) said.focus({ preventScroll: true }); // the button is gone: focus goes to what it found
  } catch (err) {
    box.classList.add("is-error");
    box.replaceChildren(el("p", "files__verdict", err.message));
    button.disabled = false;
    button.textContent = t("source.peek");
  }
}

function verdict(res) {
  const discs = res.discs > 1 ? t("files.onDiscs", { n: num(res.discs) }) : "";
  if (res.image) return t("files.image", { discs });
  if (!res.tracks) return t("files.noAudio");
  const n = `${tn("tracks.count", res.tracks)}${discs}`;
  const e = res.edition;
  if (!e) return `${n}.`;
  if (e.byCount) return t("files.byCount", { n, name: editionLabel(e) });
  if (e.exact) return t("files.exact", { name: editionLabel(e), discs }); // its name counts the tracks
  const some = (list) => `${list.slice(0, 6).join(", ")}${list.length > 6 ? "…" : ""}`;
  const parts = [t("files.nearest", { n, name: editionLabel(e) })];
  if (e.extra?.length) parts.push(t("files.extra", { list: some(e.extra) }));
  if (e.missing?.length) parts.push(t("files.missing", { list: some(e.missing) }));
  return parts.join(" ");
}

function filesDetails(res) {
  if (!res.files?.length) return [];
  const box = el("details", "files");
  box.append(el("summary", "files__head", tn("files.count", res.files.length)));
  const list = el("ul", "files__list");
  list.append(...res.files.map((f) => {
    const li = el("li", f.audio ? "file" : "file file--other");
    li.append(el("span", "file__name", f.name), el("span", "file__size", size(f.size)));
    return li;
  }));
  box.append(list);
  return [box];
}

const waitsForSource = (r) => r.status === "waiting" || r.status === "failed" || (r.status === "check" && Boolean(r.picked));

// grab sends a release; from an album's page, it is attached to the album's request when that one waits for a source.
async function grab(item, b, album = "") {
  b.disabled = true;
  b.textContent = t("source.sending");
  const r = album && state.requests.get(album);
  const linked = r && waitsForSource(r) ? r : null;
  const body = { token: item.token };
  if (linked) body.mbid = linked.mbid;
  try {
    const res = await api("/api/sources/grab", { method: "POST", body: JSON.stringify(body) });
    fly(b.getBoundingClientRect());
    b.textContent = t("source.sent");
    if (res.request) {
      state.requests.set(res.request.mbid, res.request);
      updateBadge();
      renderRecent();
      const shown = albumItem();
      if (shown?.mbid === res.request.mbid) {
        shown.status = res.request.status;
        renderAlbumHead();
      }
      toast(t("source.sentAttached", { title: res.request.title }));
    } else {
      toast(t("source.sentClient"));
    }
  } catch (err) {
    toast(err.message);
    if (err.status === 504) { // Prowlarr went quiet: the release may have left, so no second send
      b.textContent = statusLabel("check");
      if (linked) loadRequests(); // the server attached it anyway
      return;
    }
    if (err.code === "grab.alreadySent") { b.textContent = t("source.sent"); return; } // the first tap's answer was lost
    if (err.code === "grab.notWaiting") loadRequests(); // the next tap goes unattached
    b.disabled = false;
    b.textContent = t("source.download");
  }
}

function sourceSkeleton(list, n) {
  list.replaceChildren(...Array.from({ length: n }, () => {
    const node = $("#tpl-source").content.firstElementChild.cloneNode(true);
    node.classList.add("skeleton");
    node.setAttribute("aria-hidden", "true");
    node.querySelector(".source__warn").remove();
    return node;
  }));
}

// The two places a typed search reaches the trackers: the free search page, and an album's page.
const SOURCES_UI = { list: "#sources", status: "#sources-status", notice: "sources", go: "#sources-go", help: "#sources-help", level: 2, controller: null };
const ALBUM_UI = { list: "#album-sources", status: "#album-status", notice: "album", level: 3, controller: null, after: (items) => { renderAlbumNames(items); state.albumSources = items; renderUpgrade(items); } };
const LOSSLESS_CODECS = /^(flac|alac|wav|aiff?|ape|wavpack|wv|dsd)/i;

// renderUpgrade offers, on an album you own, the best release of this very album that clearly beats
// your version: lossless over lossy, more bits, or a high rate over a CD-like one (88.2 to 96 kHz is no
// upgrade). Replacing it is the download: the import keeps the better copy once it lands, and Plex
// shows it instead of the old one.
function renderUpgrade(items) {
  const box = $("#album-upgrade");
  const o = state.albumData?.owned;
  const mine = o && { lossless: LOSSLESS_CODECS.test(o.codec || ""), bits: o.bits || 0, hz: o.rate || 0 };
  const beats = (it) => {
    const q = it.quality, hz = parseFloat(q.khz || "0") * 1000;
    if (!q.lossless) return false;
    if (!mine.lossless) return true;
    return (q.bits || 0) > (mine.bits || 16) || ((q.bits || 0) === mine.bits && mine.hz && mine.hz <= 48000 && hz >= 88200); // unknown bits count as a CD's
  };
  const best = mine && (items || []).find((it) => it.place === "album" && it.seeders !== 0 && beats(it)); // the list comes best first
  box.hidden = !best;
  if (!best) return;
  const q = best.quality;
  const text = $("#album-upgrade-text");
  const what = [q.format, q.bits ? t("quality.bits", { bits: q.bits }) : "", q.khz ? t("quality.khz", { k: num(parseFloat(q.khz), 1) }) : ""];
  text.replaceChildren(t("upgrade.better"), el("b", "", what.filter(Boolean).join(" · ")),
    el("span", "album-upgrade__source", ` · ${[best.indexer, seeders(best.seeders)].filter(Boolean).join(" · ")}`));
  const b = el("button", "btn btn--blue btn--auto", t("upgrade.replace"));
  b.type = "button";
  b.setAttribute("aria-label", t("upgrade.replaceLabel", { title: best.title }));
  b.addEventListener("click", async () => {
    await grab(best, b, state.album);
    if (b.textContent === t("source.sent")) text.append(el("span", "album-upgrade__source", ` · ${t("upgrade.afterSent")}`));
  });
  $("#album-upgrade-action").replaceChildren(b);
}

async function runSources(q, ui, album = "", year = 0) {
  ui.controller?.abort();
  const status = $(ui.status);
  const list = $(ui.list);
  if ([...q].length < 2) {
    status.textContent = "";
    list.replaceChildren();
    notice(ui.notice, t("search.tooShort"));
    return;
  }
  if (ui.help) $(ui.help).hidden = true;
  if (ui === ALBUM_UI) $("#album-filter").hidden = true; // the next answer draws its own
  const controller = new AbortController();
  ui.controller = controller;
  const go = ui.go ? $(ui.go) : null;
  ui.after?.(null);
  notice(ui.notice, "");
  status.textContent = t("trackers.searchingLong");
  sourceSkeleton(list, 3);
  list.setAttribute("aria-busy", "true");
  if (go) go.disabled = true;
  try {
    const data = await api(`/api/sources?q=${encodeURIComponent(q)}${year ? `&year=${year}` : ""}${album ? `&album=${album}` : ""}`, { signal: controller.signal });
    const items = data.results || [];
    const filed = items.some((it) => it.place); // the server knew the album: its releases come first
    const rows = [];
    let group = "";
    for (const item of items) {
      const g = sourceGroup(item, filed);
      if (g !== group && SPLITS[g]) rows.push(el("li", "sources-split", SPLITS[g](albumItem()?.artist)));
      group = g;
      rows.push(sourceRow(item, ui.level, album));
    }
    list.replaceChildren(...rows);
    const mine = items.filter((it) => it.place === "album").length;
    const releases = tn("sources.releases", items.length);
    status.textContent = !items.length ? "" : t(filed ? "sources.statusFiled" : "sources.status", { releases, q, mine: num(mine) });
    if (ui === ALBUM_UI) {
      state.editionFilter = "*";
      renderEditionFilter();
    }
    if (!items.length) notice(ui.notice, t("sources.noneFor", { q }));
    ui.after?.(items);
  } catch (err) {
    if (err.name === "AbortError") return;
    status.textContent = "";
    list.replaceChildren();
    notice(ui.notice, err.message, err.status === 503 ? undefined : () => runSources(q, ui, album, year)); // 503: Prowlarr is not set up
  } finally {
    if (ui.controller === controller) {
      list.setAttribute("aria-busy", "false");
      if (go) go.disabled = false;
    }
  }
}

// Where a release sits in the list: on an album's page, the album's own releases, then the artist's
// other records, then the rest; everywhere, what lacks a searched word comes last.
const SPLITS = {
  others: (artist) => (artist ? t("split.othersOf", { artist }) : t("split.others")),
  rest: () => t("split.rest"),
  loose: () => t("split.loose"),
};

function sourceGroup(item, filed) {
  if (!filed) return item.match ? "" : "loose";
  if (item.place === "album") return "";
  if (item.place === "other" || item.place === "artist") return "others";
  return item.match ? "rest" : "loose";
}

// renderEditionFilter offers one button per edition the album's releases are of, once there are two:
// a tap shows that edition's releases only. The artist's other records stay below.
function renderEditionFilter() {
  const box = $("#album-filter");
  const rows = [...$("#album-sources").querySelectorAll('.source[data-place="album"]')];
  const counts = new Map();
  const labels = new Map([["*", t("filter.all")], ["", t("filter.unconfirmed")]]);
  const eds = state.albumData?.editions || [];
  for (const r of rows) {
    counts.set(r.dataset.edition, (counts.get(r.dataset.edition) || 0) + 1);
    // a row names its pressing; the button names the edition, which holds every pressing
    const ed = r.dataset.edition && eds.find((e) => e.key === r.dataset.edition);
    if (r.dataset.edition) labels.set(r.dataset.edition, ed ? editionLabel(ed) : r.dataset.editionName);
  }
  if (!counts.has(state.editionFilter)) state.editionFilter = "*";
  // the album page's order (most pressed first), then the ones to confirm
  const known = eds.map((e) => e.key);
  const rank = (k) => (k === "" ? Infinity : known.includes(k) ? known.indexOf(k) : known.length);
  const order = [...counts.keys()].sort((x, y) => rank(x) - rank(y));
  if (order.length < 2) {
    box.hidden = true;
    box.replaceChildren();
    for (const r of rows) r.hidden = false;
    return;
  }
  const label = (k) => labels.get(k);
  box.replaceChildren(...["*", ...order].map((k) => {
    const b = el("button", "seg seg--small");
    b.type = "button";
    b.setAttribute("aria-pressed", String(state.editionFilter === k));
    b.append(el("span", "", label(k)), el("span", "seg__n", num(k === "*" ? rows.length : counts.get(k))));
    b.addEventListener("click", () => {
      state.editionFilter = k;
      renderEditionFilter();
      announce(k === "*" ? t("filter.allAnnounce", { releases: tn("sources.releases", rows.length) })
        : t("fmt.colon", { a: tn("sources.releases", counts.get(k)), b: label(k) }));
    });
    return b;
  }));
  box.hidden = false;
  for (const r of rows) r.hidden = state.editionFilter !== "*" && r.dataset.edition !== state.editionFilter;
}

// offerTrackers proposes the free tracker search under a search's results: for what the catalogue lacks.
function offerTrackers(q) {
  $("#trackers-offer").hidden = !q;
  const b = $("#trackers-go");
  b.dataset.q = q;
  b.textContent = t("trackers.offer", { q });
}

/* ---------- Tabs ---------- */

function show(tab, focus = false) {
  if (state.tab === "library" && tab !== "library") lib.scroll = window.scrollY;
  const back = state.tab !== tab && tab === "library";
  state.tab = tab;
  for (const t of document.querySelectorAll(".tab")) {
    const on = t.dataset.panel === tab;
    t.setAttribute("aria-selected", String(on));
    t.tabIndex = on ? 0 : -1;
    if (on && focus) t.focus();
    $(`#panel-${t.dataset.panel}`).hidden = !on;
  }
  try { localStorage.setItem("musaarr.tab", tab); } catch { /* private mode */ }
  if (tab === "library") {
    renderRequests();
    loadRequests();
    if (!lib.drawn) restoreLibrary();
    loadLibrary();
    if (back) returnToShelf();
  }
}

function init() {
  const tabs = [...document.querySelectorAll(".tab")];
  tabs.forEach((t, i) => {
    t.addEventListener("click", () => show(t.dataset.panel));
    t.addEventListener("keydown", (e) => {
      if (e.key !== "ArrowRight" && e.key !== "ArrowLeft") return;
      const next = tabs[(i + (e.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length];
      show(next.dataset.panel, true);
    });
  });
  $("#q").addEventListener("input", onInput);
  $("#search-form").addEventListener("submit", (e) => {
    e.preventDefault();
    clearTimeout(debounce);
    state.lastQuery = $("#q").value.trim();
    search(state.lastQuery);
    $("#q").blur();
  });
  $("#sources-form").addEventListener("submit", (e) => {
    e.preventDefault();
    runSources($("#sq").value.trim(), SOURCES_UI);
  });
  $("#trackers-go").addEventListener("click", (e) => {
    $("#sq").value = e.currentTarget.dataset.q;
    location.hash = "#trackers";
    runSources(e.currentTarget.dataset.q, SOURCES_UI);
  });
  for (const b of $("#order").querySelectorAll("button")) {
    b.addEventListener("click", () => {
      state.order = b.dataset.order;
      try { localStorage.setItem("musaarr.order", state.order); } catch { /* private mode */ }
      renderOrder();
      renderDiscography();
    });
  }
  $("#back").addEventListener("click", () => back());
  $("#home").addEventListener("click", (e) => { // the wordmark leads home: an empty search
    e.preventDefault();
    $("#q").value = "";
    state.lastQuery = "";
    state.returnTab = null;
    state.fromLibrary = false;
    state.fromShop = false;
    state.searchScroll = 0;
    search("");
    show("search");
    if (location.hash) history.pushState(null, "", location.pathname + location.search);
    route();
    window.scrollTo(0, 0);
  });
  $("#album-back").addEventListener("click", () => {
    if (state.albumFromArtist || ((state.fromLibrary || state.fromShop) && state.navigated)) { history.back(); return; }
    if (state.fromLibrary) { history.replaceState(null, "", location.pathname + location.search); route(); return; }
    history.replaceState(null, "", `#artiste/${state.artist.mbid}`); // opened from a request or a link: its artist is the way up
    route();
  });
  $("#sources-back").addEventListener("click", () => back());
  $("#go-search").addEventListener("click", () => { show("search"); $("#q").focus(); });
  $("#see-all").addEventListener("click", () => show("library", true));
  window.addEventListener("hashchange", () => { state.navigated = true; route(); });
  document.addEventListener("visibilitychange", () => {
    schedule();
    if (document.visibilityState !== "visible") return;
    loadRequests();
    if (state.tab === "library") loadLibrary();
  });
  initShelf();
  initRoom();
  initCrate();
  let saved = "search";
  try {
    saved = localStorage.getItem("musaarr.tab") || "search";
    if (localStorage.getItem("musaarr.order") === "date") state.order = "date";
  } catch { /* private mode */ }
  show(saved === "library" || saved === "requests" ? "library" : "search"); // "requests" until 1.7, "sources" a tab until 1.4

  restoreRequests();
  // route runs after rack.js, which opens the shop in 3D: both wait for the words, rack.js second.
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", route, { once: true });
  else i18nReady.then(route);
  loadRequests();
  loadSuggestions();
  loadStage();
  schedule();
  $("#offline-retry").addEventListener("click", loadRequests);
  const lang = $("#lang"); // each language under its own name
  lang.replaceChildren(...Object.entries(i18n.langs).map(([code, name]) => new Option(name, code, false, code === i18n.lang)));
  lang.addEventListener("change", () => setLang(lang.value));
  if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => { /* the app works without it */ });
}

i18nReady.then(init);
