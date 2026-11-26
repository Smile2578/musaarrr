"use strict";
/* ---------- Le disquaire en racks ----------
   The server files the shop's records (GET /api/shop/racks): a rack per genre, crates of 12 records
   at most by letter, a kraft divider before an artist with several. The page reads the index of the
   racks first (?summary=1), then a rack's records (?rack=<id>) as it comes near, and fills a crate
   only as it nears the row's edge: tens of thousands of records stay light. Here they are drawn and dug:
   the wheel, a finger on the tops of the records or the arrow keys flip a crate; the rows scroll
   sideways. Uses app.js's helpers ($, el, api, fr, announce, reduceMotion, onInput…). */

const BEHIND = 8; // records drawn behind the front one
const FLIPPED = 2; // records kept once flipped forward, so flipping back moves them
const FLIP_PX = 36; // a drag of this many pixels flips one record
const INKS = ["blue", "ochre", "vermilion", "green", "paper"];
const shop = { racks: [], crateWatch: [], crates: new Map(), loaded: 0, timer: 0, tries: 0, asks: 0, hide: false, hold: false, pageWheelAt: 0, sayTimer: 0, from: null, aisleWatch: null, nearWatch: null };

const disques = (n) => tn("records.count", n);
// A rack the catalogue names is read in the reader's language; another keeps the server's name.
const rackName = (r) => (i18n.has(`rack.${r.id}`) ? t(`rack.${r.id}`) : r.name);
const hashOf = (s) => [...s].reduce((h, c) => (h * 31 + c.codePointAt(0)) >>> 0, 7);
const motion = () => (reduceMotion.matches ? "auto" : "smooth");

async function loadShop() {
  clearTimeout(shop.timer);
  const ask = ++shop.asks; // the box ticked twice quickly: only the last answer is drawn
  try {
    const data = await api(`/api/shop/racks?summary=1${shop.hide ? "&hide=1" : ""}`);
    if (ask !== shop.asks) return;
    shop.racks = (data.racks || []).map((r) => ({ ...r, name: rackName(r) }));
    if ($("#shop").classList.contains("is-3d")) renderFront();
    else renderRacks(!shop.loaded);
    shop.loaded = Date.now();
    if (shop3d && !$("#shop3d").hidden) await enter3D($("#shop3d"));
  } catch (err) {
    if (ask !== shop.asks || shop.loaded) return; // the last stock stays on the page
    $("#shop-opening").hidden = true;
    const wait = $("#shop-wait");
    wait.textContent = err.message;
    wait.hidden = false;
    if (err.status === 503 && shop.tries++ < 20) shop.timer = setTimeout(loadShop, 90000); // the NAS is filling it
  }
}

// rackOf reads a rack's records once, kept on the index's rack as r.full; null when it failed (said
// in a toast, and asked again when the rack comes near again) or when the stock changed meanwhile.
function rackOf(r) {
  r.loading ??= api(`/api/shop/racks?rack=${encodeURIComponent(r.id)}${shop.hide ? "&hide=1" : ""}`).then((full) => {
    if (full.crates?.length !== r.crates.length) { // rebuilt since the index: read it again
      loadShop();
      return null;
    }
    return (r.full = full);
  }, (err) => {
    r.loading = null;
    toast(err.message);
    return null;
  });
  return r.loading;
}

// enter3D opens the 3D shop on the index: it reads a rack's records (rackOf) when you come near its aisle.
const enter3D = (root) => shop3d.enter({ root, racks: shop.racks, rackOf, hide: shop.hide, setHide, exit: exitShop3D, openAlbum: albumFrom3D });

// renderFront says on the shop's door what is inside; the room itself opens on #disquaire.
function renderFront() {
  const n = shop.racks.reduce((sum, r) => sum + (r.id === "mois" ? 0 : r.n), 0); // this month's records are also in their genre
  $("#shop-front-count").textContent = n ? `${tn("shop.racks", shop.racks.length)} · ${disques(n)}` : "";
  const wait = $("#shop-wait");
  wait.hidden = n > 0;
  wait.textContent = n ? "" : t(shop.hide ? "shop.allOwned" : "shop.empty");
}

// renderRacks draws the racks; the first time, behind the opening while the first covers come.
function renderRacks(opening = false) {
  shop.drawn = shop.racks;
  shop.crates.clear();
  shop.aisleWatch?.disconnect();
  shop.nearWatch?.disconnect();
  shop.crateWatch.forEach((w) => w.disconnect());
  shop.crateWatch = [];
  const n = shop.racks.reduce((sum, r) => sum + r.n, 0);
  shop.hold = opening && n > 0; // while the shop opens, only the first screen's covers are asked for
  const sections = shop.racks.map(rackSection);
  $("#shop-racks").replaceChildren(...sections);
  $("#aisles").replaceChildren(...shop.racks.map(aisleButton));
  $("#aisles").hidden = shop.racks.length < 2;
  $("#shop-random").hidden = n === 0;
  const wait = $("#shop-wait");
  wait.hidden = n > 0;
  wait.textContent = n ? "" : t(shop.hide ? "shop.allOwned" : "shop.empty");
  if (shop.hold) {
    Promise.all(sections.slice(0, 2).map(openRack)).then(() => shopOpening(sections)).then(() => watchRacks(sections));
    return;
  }
  $("#shop-opening").hidden = true;
  watchRacks(sections);
}

function watchRacks(sections) {
  if (!("IntersectionObserver" in window)) {
    sections.forEach(openRack);
    return;
  }
  shop.aisleWatch = new IntersectionObserver((entries) => { // the rack crossing the upper third is the one you are in
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      const bar = $("#aisles");
      for (const b of bar.children) {
        const on = b.dataset.rack === e.target.dataset.rack;
        b.setAttribute("aria-current", String(on));
        if (on) bar.scrollLeft = b.offsetLeft - (bar.clientWidth - b.offsetWidth) / 2; // never scrollIntoView: it would stop the page's own scroll
      }
    }
  }, { rootMargin: "-30% 0px -60% 0px" });
  shop.nearWatch = new IntersectionObserver((entries) => { // a rack coming near reads its records
    for (const e of entries.filter((x) => x.isIntersecting)) {
      shop.nearWatch.unobserve(e.target);
      openRack(e.target).then((ok) => ok || setTimeout(() => shop.nearWatch?.observe(e.target), 30000));
    }
  }, { rootMargin: "1200px 0px" });
  sections.forEach((s) => { shop.aisleWatch.observe(s); shop.nearWatch.observe(s); });
}

// openRack fills a rack's crates once its records are read: the first two screens of the row at
// once (the opening shows them), the others as they near the row's edge. Says whether it could.
async function openRack(sec) {
  if (sec.dataset.open) return true;
  sec.dataset.open = "1";
  const r = shop.racks.find((x) => x.id === sec.dataset.rack);
  const full = r && (await rackOf(r));
  if (!full || !sec.isConnected) {
    delete sec.dataset.open;
    return false;
  }
  const row = sec.querySelector(".srack__row");
  const fill = (node) => {
    const cr = shop.crates.get(node.dataset.key);
    fillCrate(cr, full.crates[cr.i]);
  };
  if (!("IntersectionObserver" in window)) {
    [...row.children].forEach(fill);
    return true;
  }
  const watch = new IntersectionObserver((entries) => {
    for (const e of entries.filter((x) => x.isIntersecting)) {
      fill(e.target);
      watch.unobserve(e.target);
    }
  }, { root: row, rootMargin: "0px 100%" });
  for (const node of row.children) {
    if (node.offsetLeft - row.scrollLeft < 2 * row.clientWidth) fill(node);
    else watch.observe(node);
  }
  shop.crateWatch.push(watch);
  return true;
}

// shopOpening shows the shop unpacking while the covers of the first screen load, then lets those
// crates in together, so no cover pops up on its own. Six seconds at most: a slow cover arrives later.
async function shopOpening(sections) {
  const box = $("#shop-racks");
  box.classList.add("is-opening");
  const first = sections.slice(0, 2).flatMap((s) => {
    const row = s.querySelector(".srack__row");
    return [...row.children].filter((c) => c.offsetLeft < row.clientWidth);
  });
  first.forEach((c) => showCovers(shop.crates.get(c.dataset.key), true));
  const imgs = first.flatMap((c) => [...c.querySelectorAll(".lp__sleeve img[src]")]);
  const timeout = new Promise((done) => setTimeout(done, 6000));
  await Promise.race([Promise.all(imgs.map((img) => img.decode().catch(() => {}))), timeout]);
  shop.hold = false;
  for (const cr of shop.crates.values()) showCovers(cr);
  first.forEach((c, i) => {
    c.style.setProperty("--i", String(i % 8));
    c.classList.add("is-arriving");
    c.addEventListener("animationend", () => c.classList.remove("is-arriving"), { once: true });
  });
  box.classList.remove("is-opening");
  $("#shop-opening").hidden = true;
}

function aisleButton(r) {
  const b = el("button", "aisle");
  b.type = "button";
  b.dataset.rack = r.id;
  b.setAttribute("aria-current", "false");
  b.append(el("span", "", r.name), el("span", "aisle__n", num(r.n)));
  b.setAttribute("aria-label", `${r.name}, ${disques(r.n)}`);
  b.addEventListener("click", () => $(`#rack-${r.id}`).scrollIntoView({ block: "start", behavior: motion() }));
  return b;
}

function rackSection(r) {
  const sec = el("section", "srack");
  sec.id = `rack-${r.id}`;
  sec.dataset.rack = r.id;
  sec.setAttribute("aria-labelledby", `rack-${r.id}-h`);
  const h = el("h3", "srack__title", r.name);
  h.id = `rack-${r.id}-h`;
  h.append(el("span", "srack__n", disques(r.n)));
  const row = el("div", "srack__row");
  const crates = r.crates.map((c, i) => crateNode(r, c, i));
  row.append(...crates.map((cr) => cr.el));
  const letters = el("div", "srack__letters");
  letters.setAttribute("role", "group");
  letters.setAttribute("aria-label", t("shop.cratesOf", { name: r.name }));
  // a long rack gets a button per letter, at its first crate; a short one, a button per crate
  const initial = (cr) => [...cr.data.label][0];
  const marks = crates.length <= 26 ? crates : crates.filter((cr, i) => i === 0 || initial(cr) !== initial(crates[i - 1]));
  letters.append(...marks.map((cr) => {
    const one = marks === crates;
    const b = el("button", "srack__letter", one ? cr.data.label : initial(cr));
    b.type = "button";
    b.dataset.from = String(cr.i);
    b.setAttribute("aria-label", one ? t("shop.goToCrate", { label: cr.data.label }) : t("shop.goToLetter", { letter: initial(cr) }));
    b.addEventListener("click", () => row.scrollTo({ left: cr.el.offsetLeft - crates[0].el.offsetLeft, behavior: motion() }));
    return b;
  }));
  const arrows = sideArrows(row, t("shop.cratesOf", { name: r.name }));
  const head = el("div", "srack__head");
  head.append(h, letters, arrows);
  sec.append(head, row);
  bindRow(sec, row, crates);
  return sec;
}

// rowSync marks the letter of the crate at the row's left edge. The row is the crates' offset
// parent: their offsetLeft does not move as it scrolls.
function rowSync(sec, row, crates) {
  const here = Math.max(0, crates.findIndex((cr) => cr.el.offsetLeft + cr.el.offsetWidth / 2 > row.scrollLeft));
  const marks = [...sec.querySelector(".srack__letters").children];
  const on = marks.findLast((b) => Number(b.dataset.from) <= here);
  marks.forEach((b) => b.classList.toggle("is-here", b === on));
}

function bindRow(sec, row, crates) {
  let queued = false;
  row.addEventListener("scroll", () => {
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => { queued = false; rowSync(sec, row, crates); });
  }, { passive: true });
  requestAnimationFrame(() => rowSync(sec, row, crates));
  bindDig(row);
}

// bindDig: a mouse drag sideways slides the row, up or down it flips the crate under it. A finger
// slides the row natively, and flips only from the tops of the records (their touch-action).
function bindDig(row) {
  let d = null;
  row.addEventListener("pointerdown", (e) => {
    if (e.button !== 0) return;
    const cr = shop.crates.get(e.target.closest(".bac")?.dataset.key);
    const rec = e.target.closest(".lp");
    const mouse = e.pointerType === "mouse";
    const tops = Boolean(e.target.closest(".lp__tab, .lp:not(.is-front):not(.is-flipped)"));
    d = { id: e.pointerId, x: e.clientX, y: e.clientY, left: row.scrollLeft, mode: "", cr, mouse,
      k: rec ? Number(rec.dataset.k) : -1, tab: Boolean(e.target.closest(".lp__tab")),
      dig: Boolean(cr && e.target.closest(".bac__well")) && (mouse || tops) };
  });
  row.addEventListener("pointermove", (e) => {
    if (!d || d.id !== e.pointerId) return;
    if (!(e.buttons & 1)) { end(e); return; } // released outside the row
    const dx = e.clientX - d.x, dy = e.clientY - d.y;
    if (!d.mode && Math.max(Math.abs(dx), Math.abs(dy)) > 8) {
      d.mode = Math.abs(dx) > Math.abs(dy) ? (d.mouse ? "slide" : "none") : d.dig ? "flip" : "none";
      if (d.mode !== "none") row.setPointerCapture(e.pointerId);
      row.classList.toggle("is-sliding", d.mode === "slide");
      d.cr?.well.classList.toggle("is-held", d.mode === "flip");
    }
    if (d.mode === "slide") row.scrollLeft = d.left - dx;
    if (d.mode !== "flip") return;
    const steps = Math.trunc(dy / FLIP_PX); // pulling the records toward you shows the next one
    if (steps) {
      bacGo(d.cr, d.cr.f + steps);
      d.y += steps * FLIP_PX;
    }
  });
  const end = (e) => {
    if (!d || d.id !== e.pointerId) return;
    const done = d;
    d = null;
    row.classList.remove("is-sliding");
    done.cr?.well.classList.remove("is-held");
    if (e.type === "pointerup" && !done.mode && done.cr && done.k >= 0) bacTap(done.cr, done.k, done.tab);
  };
  row.addEventListener("pointerup", end);
  row.addEventListener("pointercancel", end);
  row.addEventListener("dragstart", (e) => e.preventDefault());
}

/* A crate */

function crateNode(r, c, i) {
  const key = `${r.id}/${i}`;
  const node = el("div", "bac");
  node.dataset.key = key;
  const box = el("div", "bac__box");
  box.tabIndex = 0;
  box.setAttribute("role", "group");
  box.setAttribute("aria-roledescription", t("shop.crate"));
  box.setAttribute("aria-label", t("shop.crateLabel", { rack: r.name, label: c.label, records: disques(c.n) }));
  box.setAttribute("aria-describedby", "bac-help");
  const well = el("div", "bac__well");
  const lip = el("div", "bac__lip");
  lip.setAttribute("aria-hidden", "true");
  const sticker = el("span", "bac__sticker");
  sticker.append(el("span", "bac__label", c.label), el("span", "bac__count", disques(c.n)));
  lip.append(sticker);
  box.append(well, lip);
  const cap = { title: el("p", "bac__title"), meta: el("p", "bac__meta"), pos: el("p", "bac__pos") };
  const caption = el("div", "bac__cap");
  caption.append(cap.title, cap.meta, cap.pos);
  node.append(box, caption);
  const cr = { key, i, rack: r, data: c, items: [], nodes: [], nth: [], f: 0, el: node, box, well, cap, wheel: 0, wheelAt: 0 };
  well.addEventListener("wheel", (e) => bacWheel(cr, e), { passive: false });
  box.addEventListener("keydown", (e) => bacKey(cr, e));
  shop.crates.set(key, cr);
  return cr;
}

// fillCrate puts the records in a crate drawn empty, once its rack's records are read (rackOf).
function fillCrate(cr, c) {
  if (!cr || cr.items.length || !c?.items?.length) return;
  let nth = 0;
  cr.items = c.items;
  cr.nth = c.items.map((it) => (it.record ? ++nth : nth));
  cr.f = Math.max(0, c.items.findIndex((it) => it.record));
  cr.nodes = c.items.map(recNode);
  cr.well.append(...cr.nodes);
  for (const img of cr.well.querySelectorAll("img")) img.loading = "eager"; // off to the side, lazy would wait for the row to scroll
  bacLayout(cr, true);
  bacCaption(cr);
}

function recNode(it, k, items) {
  const node = el("div", `lp${it.divider ? " lp--div" : ""}`);
  node.dataset.k = String(k);
  if (!it.divider) {
    node.append(sleeveOf(it, node));
    return node;
  }
  const card = el("div", "lp__card");
  const sort = items[k + 1]?.record?.sort; // « Takanaka, Masayoshi » under « 高中正義 »
  card.append(el("p", "lp__big", it.divider), el("p", "lp__small", sort ? `${sort} · ${disques(it.n)}` : disques(it.n)));
  const tab = el("span", "lp__tab", it.divider);
  tab.style.setProperty("--tab-x", `${6 + (hashOf(it.divider) % 28)}%`); // tabs stagger, as in a real crate
  node.append(card, tab);
  return node;
}

// sleeveOf is a rack item's sleeve: the front of its Japanese pressing when known, else its cover,
// over a printed sleeve that shows while the picture comes, or if there is none. Given a holder,
// the picture waits in its data-src until the record nears the front.
function sleeveOf(it, holder = null) {
  const r = it.record;
  const box = el("div", "lp__sleeve");
  box.append(drawnSleeve(r, it.color));
  const srcs = [it.jp ? `/api/shop/${r.mbid}/cover?jp=1` : "", r.cover || ""].filter(Boolean);
  if (!srcs.length) return box;
  const img = el("img");
  img.alt = "";
  img.decoding = "async";
  img.loading = "lazy";
  img.draggable = false;
  img.addEventListener("load", () => img.classList.add("is-in")); // a late cover fades in over its printed sleeve
  img.addEventListener("error", () => {
    srcs.shift();
    if (srcs.length) img.src = srcs[0];
    else img.remove();
  });
  if (holder) holder.dataset.src = srcs[0];
  else img.src = srcs[0];
  box.append(img);
  return box;
}

// drawnSleeve is printed in the cover's own colour once the server has read it, so the picture
// fades in over its own tint; else in a spot ink.
function drawnSleeve(r, color) {
  const d = el("div", "lp__drawn");
  if (/^#[0-9a-f]{6}$/.test(color || "")) {
    const ink = inkOf(color);
    d.style.setProperty("--ink-bg", ink.bg);
    d.style.setProperty("--ink-fg", ink.fg);
  } else d.dataset.ink = INKS[hashOf(r.title + r.artist) % INKS.length];
  d.append(el("span", "lp__drawn-title", r.title), el("span", "lp__drawn-artist", r.artist));
  return d;
}

const DARK_INK = [16, 16, 18]; // the paper sleeve's #101012 and #f2f2ee
const LIGHT_INK = [242, 242, 238];
const luminance = (rgb) => rgb.reduce((sum, v, i) => {
  const c = v / 255;
  return sum + [0.2126, 0.7152, 0.0722][i] * (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
}, 0);
const contrast = (a, b) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

// inkOf is a sleeve printed in a cover's colour: the text that reads best on it, the colour pushed
// lighter or darker until that text has 4.5:1 (WCAG AA).
function inkOf(hex) {
  let bg = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const fg = contrast(bg, DARK_INK) > contrast(bg, LIGHT_INK) ? DARK_INK : LIGHT_INK;
  const away = fg === DARK_INK ? 255 : 0;
  for (let i = 0; i < 20 && contrast(bg, fg) < 4.5; i++) bg = bg.map((v) => Math.round(v + (away - v) * 0.15));
  return { bg: `rgb(${bg.join(" ")})`, fg: `rgb(${fg.join(" ")})` };
}

function bacLayout(cr, still = false) {
  cr.well.classList.toggle("is-still", still);
  cr.nodes.forEach((node, k) => {
    const d = k - cr.f;
    const divider = Boolean(cr.items[k].divider);
    node.style.setProperty("--d", String(Math.max(0, Math.min(d, BEHIND))));
    node.classList.toggle("is-deep", d > BEHIND); // a far divider waits behind the last record drawn, its tab still up
    node.classList.toggle("is-front", d === 0);
    node.classList.toggle("is-flipped", d < 0);
    node.classList.toggle("is-far", d < -FLIPPED || (d > BEHIND && !divider));
  });
  if (!shop.hold) showCovers(cr);
  if (still) requestAnimationFrame(() => cr.well.classList.remove("is-still"));
}

// showCovers gives the records near the front their picture; first, for the opening, asks them at once.
function showCovers(cr, first = false) {
  cr.nodes.forEach((node, k) => {
    const d = k - cr.f;
    if (d < -1 || d > BEHIND || !node.dataset.src) return;
    const img = node.querySelector("img");
    img.fetchPriority = d === 0 || first ? "high" : "low"; // the front covers first, then the edges behind them
    if (first) img.loading = "eager";
    img.src = node.dataset.src;
    delete node.dataset.src;
  });
}

// shopItem is a record as an album card sees it: its state comes from the user's requests first.
function shopItem(r, owned) {
  const req = state.requests.get(r.mbid);
  return {
    mbid: r.mbid, title: r.title, artist: r.artist, artistMbid: r.artistMbid, year: r.year, cover: r.cover,
    status: req?.status || (owned || r.plex ? "in_plex" : ""), progress: req?.progress || 0, timeleft: req?.timeleft || "", plexUrl: req?.plexUrl,
  };
}

function bacCaption(cr) {
  const it = cr.items[cr.f];
  if (!it) return; // its records are on their way
  const { title, meta, pos } = cr.cap;
  if (it.divider) {
    title.textContent = it.divider;
    meta.replaceChildren(t("shop.divider"));
    pos.textContent = t("shop.behind", { records: disques(it.n) });
    return;
  }
  const r = it.record;
  const item = shopItem(r, it.owned);
  title.textContent = r.title;
  meta.replaceChildren([r.artist, r.year].filter(Boolean).join(" · "));
  const mine = item.status === "in_plex" ? t("shop.owned") : statusText(item);
  if (mine) meta.append(" · ", el("span", item.status === "in_plex" ? "bac__mine" : "", mine));
  const n = cr.nth[cr.f];
  pos.textContent = t("crate.pos", { i: num(n), n: num(cr.data.n) }) + (n === cr.data.n && n > 1 ? ` · ${t("crate.bottom")}` : "");
}

// bacGo brings item k to the front; the position is read out once the flicking stops.
function bacGo(cr, k, still = false) {
  const to = Math.max(0, Math.min(cr.items.length - 1, k));
  if (to === cr.f) return;
  cr.f = to;
  bacLayout(cr, still);
  bacCaption(cr);
  clearTimeout(shop.sayTimer);
  shop.sayTimer = setTimeout(() => {
    const { title, meta, pos } = cr.cap;
    announce(`${title.textContent}, ${meta.textContent}, ${pos.textContent}`);
  }, 450);
}

// bacTap: a tab or a divider in front leads to the artist's first record, the front record opens
// its album page, a record behind comes to the front.
function bacTap(cr, k, tab) {
  if (!cr.items.length) return;
  if (tab || (k === cr.f && cr.items[k].divider)) bacGo(cr, k + 1);
  else if (k === cr.f) openRecord(cr);
  else bacGo(cr, k);
  cr.box.focus({ preventScroll: true });
}

// bacWheel flips one record per wheel notch. A wheel that was scrolling the page keeps
// scrolling it as the pointer crosses a crate, and so does one that would flip past an end.
function bacWheel(cr, e) {
  if (e.ctrlKey || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return; // a zoom, or the row going sideways
  const now = performance.now();
  const edge = e.deltaY < 0 ? cr.f === 0 : cr.f === cr.items.length - 1;
  if (edge || now - shop.pageWheelAt < 350) {
    shop.pageWheelAt = now;
    return;
  }
  e.preventDefault();
  if (now - cr.wheelAt > 220) cr.wheel = 0;
  cr.wheelAt = now;
  cr.wheel += e.deltaMode === 1 ? e.deltaY * 40 : e.deltaY;
  if (Math.abs(cr.wheel) >= 50) { // a mouse notch is 100 pixels: one record; a trackpad adds up to 50
    bacGo(cr, cr.f + Math.sign(cr.wheel) * Math.max(1, Math.round(Math.abs(cr.wheel) / 100)));
    cr.wheel = 0;
  }
}

function nextDivider(cr, dir) {
  for (let k = cr.f + dir; k >= 0 && k < cr.items.length; k += dir) if (cr.items[k].divider) return k;
  return dir > 0 ? cr.items.length - 1 : 0;
}

function bacKey(cr, e) {
  const k = { ArrowDown: cr.f + 1, ArrowUp: cr.f - 1, Home: 0, End: cr.items.length - 1 }[e.key]
    ?? (e.key === "PageDown" || e.key === "PageUp" ? nextDivider(cr, e.key === "PageDown" ? 1 : -1) : undefined);
  if (k !== undefined) {
    e.preventDefault();
    bacGo(cr, k);
  } else if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
    e.preventDefault();
    const next = shop.crates.get(`${cr.rack.id}/${cr.i + (e.key === "ArrowRight" ? 1 : -1)}`);
    if (!next) return;
    fillCrate(next, cr.rack.full?.crates[next.i]);
    next.box.focus({ preventScroll: true });
    next.el.scrollIntoView({ inline: "nearest", block: "nearest", behavior: motion() });
  } else if (e.key === "Enter" || e.key === " ") {
    e.preventDefault();
    bacTap(cr, cr.f, false);
  }
}

// shopRandom brings a record the user does not have to the front of its crate: the evening's find,
// in a rack drawn by its size, then anywhere in it.
async function shopRandom() {
  const racks = shop.racks.filter((r) => r.id !== "mois");
  let at = Math.random() * racks.reduce((sum, r) => sum + r.n, 0);
  const r = racks.find((x) => (at -= x.n) < 0) ?? racks.at(-1);
  const full = r && (await rackOf(r));
  if (!full) return;
  const all = [];
  full.crates.forEach((c, i) => c.items.forEach((it, k) => { if (it.record && !it.owned) all.push([i, k]); }));
  if (!all.length) return;
  const [i, k] = all[Math.floor(Math.random() * all.length)];
  const cr = shop.crates.get(`${r.id}/${i}`);
  if (!cr) return;
  fillCrate(cr, full.crates[i]);
  bacGo(cr, k, true);
  const row = cr.el.parentElement;
  row.scrollLeft = cr.el.offsetLeft - (row.clientWidth - cr.el.offsetWidth) / 2;
  cr.el.closest(".srack").scrollIntoView({ block: "start", behavior: motion() });
  cr.box.focus({ preventScroll: true });
  announce(t("shop.random", { title: cr.items[k].record.title, artist: cr.items[k].record.artist }));
}

// shopReturn puts the focus back on the crate an album page was opened from, its caption
// showing a request made on that page.
function shopReturn() {
  if (!shop.from?.box.isConnected) return;
  bacCaption(shop.from);
  shop.from.box.focus({ preventScroll: true });
}

// openRecord opens the front record's album page; Back returns to its crate.
function openRecord(cr) {
  const r = cr.items[cr.f].record;
  if (!r.artistMbid) { // no artist to open the album under: look it up by name
    $("#q").value = `${r.artist} ${r.title}`;
    onInput();
    return;
  }
  shop.from = cr;
  state.fromShop = true;
  location.hash = `#artiste/${r.artistMbid}/album/${r.mbid}`;
}

/* ---------- La boutique en 3D (shop3d/main.js, loaded on the first visit) ---------- */

const can3D = (() => { try { return !!document.createElement("canvas").getContext("webgl2"); } catch { return false; } })();
let shop3d = null;

function wants3D() {
  try { return can3D && localStorage.getItem("musaarr.disquaire") !== "bacs"; } catch { return can3D; }
}

// shopMode puts the shop front (3D) or the racks (no 3D, or asked for) on the home page.
function shopMode(three) {
  try { localStorage.setItem("musaarr.disquaire", three ? "3d" : "bacs"); } catch { /* private mode */ }
  $("#shop").classList.toggle("is-3d", three);
  $("#shop-to-3d").hidden = three || !can3D;
  if (!three && shop.racks.length && shop.drawn !== shop.racks) renderRacks(!shop.drawn); // the stock changed while in 3D
}

function setHide(on) {
  shop.hide = on;
  $("#shop-hide").checked = on;
  try { localStorage.setItem("musaarr.cacher", on ? "1" : "0"); } catch { /* private mode */ }
  loadShop();
}

let shopVisit = 0; // a close during the loading cancels that opening

async function openShop3D() {
  const root = $("#shop3d");
  const mine = ++shopVisit;
  root.hidden = false;
  document.documentElement.classList.add("is-in-shop");
  state.fromShop = false;
  try {
    if (!shop.loaded) await loadShop();
    shop3d ??= await import("/shop3d/main.js");
    if (mine !== shopVisit) return;
    await enter3D(root);
  } catch (err) {
    console.error("3D shop:", err);
    if (mine !== shopVisit) return; // already left: the page shown now is not the shop's
    closeShop3D();
    history.replaceState(null, "", location.pathname + location.search); // so "Enter" can try again
    shopMode(false);
    toast(t("shop.no3d"));
  }
}

function closeShop3D() {
  if ($("#shop3d").hidden) return;
  shopVisit++;
  $("#shop3d").hidden = true;
  document.documentElement.classList.remove("is-in-shop");
  shop3d?.leave();
}

function exitShop3D() {
  if (state.navigated) history.back();
  else {
    history.replaceState(null, "", location.pathname + location.search);
    route();
  }
  requestAnimationFrame(() => $("#shop-enter").focus({ preventScroll: true }));
}

function albumFrom3D(r) {
  state.fromShop = true;
  if (r.artistMbid) location.hash = `#artiste/${r.artistMbid}/album/${r.mbid}`;
  else {
    closeShop3D();
    $("#q").value = `${r.artist} ${r.title}`;
    onInput();
  }
}

function initRacks() {
  try { shop.hide = localStorage.getItem("musaarr.cacher") === "1"; } catch { /* private mode */ }
  const hide = $("#shop-hide");
  hide.checked = shop.hide;
  hide.addEventListener("change", () => setHide(hide.checked));
  $("#shop-flat").addEventListener("click", () => shopMode(false));
  $("#shop-to-3d").addEventListener("click", () => shopMode(true));
  shopMode(wants3D());
  $("#shop-random").addEventListener("click", shopRandom);
  addEventListener("wheel", (e) => { if (!e.target.closest?.(".bac__well")) shop.pageWheelAt = performance.now(); }, { passive: true });
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible" && shop.loaded && Date.now() - shop.loaded > 6 * 3600e3) loadShop(); // rebuilt daily
  });
  loadShop();
}

i18nReady.then(initRacks);
