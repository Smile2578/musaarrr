// The first run in a browser, against a fake Lidarr, a fake Plex and a fake plex.tv: the admin
// account, Lidarr tested (a wrong key first) and saved, Plex by a code, then the app.
// Needs Go and Playwright with Chromium: node --test test/e2e/*.test.mjs
// E2E_SHOTS=<folder> keeps a screenshot of each step.
import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { createRequire } from "node:module";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const { chromium } = createRequire(import.meta.url)("playwright");
const root = new URL("../..", import.meta.url).pathname;
const words = (lang) => JSON.parse(readFileSync(join(root, "web/i18n", `${lang}.json`), "utf8"));
const fill = (s, p = {}) => s.replace(/\{(\w+)\}/g, (h, k) => p[k] ?? h);

// serve starts TestWizardServer on port and resolves with its fakes' addresses once it listens.
function serve(binary, port) {
  const proc = spawn(binary, ["-test.run", "^TestWizardServer$", "-test.v", "-test.timeout", "6m"], {
    cwd: root, env: { ...process.env, MUSAARR_E2E_ADDR: `127.0.0.1:${port}` },
  });
  return new Promise((resolve, reject) => {
    let out = "";
    proc.on("exit", (code) => reject(new Error(`server exited (${code}): ${out}`)));
    proc.stderr.on("data", (d) => { out += d; });
    proc.stdout.on("data", (d) => {
      out += d;
      const m = out.match(/e2e lidarr=(\S+) plex=(\S+) control=(\S+)/);
      if (m) resolve({ proc, base: `http://127.0.0.1:${port}`, lidarr: m[1], plex: m[2], control: m[3] });
    });
  });
}

async function walk(browser, s, lang, device, shots) {
  const w = words(lang);
  const errors = [];
  const ctx = await browser.newContext({ ...device, locale: lang === "fr" ? "fr-FR" : "en-US" });
  await ctx.route(/\/api\/(sources|artists\/[^/]+\/sources)/, (r) => r.abort()); // never near an indexer
  await ctx.route("https://app.plex.tv/**", async (r) => { // the admin approves the code on plex.tv
    await fetch(`${s.control}/approve`, { method: "POST" });
    await r.fulfill({ contentType: "text/html", body: "<title>plex.tv</title>approved" });
  });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => { // a refused key is a 400 on purpose, and the fake Plex holds no music
    if (m.type() === "error" && !m.text().startsWith("Failed to load resource")) errors.push(m.text());
  });
  const shot = async (name) => shots && page.screenshot({ path: join(shots, `${lang}-${name}.png`), fullPage: true });

  await page.goto(s.base + "/");
  await page.waitForSelector("#form:not([hidden])");
  assert.equal(new URL(page.url()).pathname, "/login.html");
  await page.fill("#name", "admin");
  await page.fill("#password", "long enough");
  await page.click("#submit");
  await page.waitForURL(s.base + "/setup.html");
  await page.waitForSelector("#page:not([hidden])");
  assert.equal(await page.textContent("#title"), w["setup.heading.first"]);
  assert.equal(await page.isVisible("#plex"), false, "Plex waits for Lidarr");
  await shot("1-lidarr");

  await page.fill("#lidarr-url", s.lidarr);
  await page.fill("#lidarr-key", "wrong");
  await page.click("#lidarr-test");
  await page.waitForSelector("#lidarr-status.is-error");
  assert.equal(await page.textContent("#lidarr-status"), w["error.setup.lidarrBadKey"]);
  assert.equal(await page.getAttribute("#lidarr-key", "aria-invalid"), "true");
  await shot("2-wrong-key");

  await page.fill("#lidarr-key", "secret-key");
  await page.click("#lidarr-test");
  await page.waitForSelector("#choice-form:not([hidden])");
  assert.equal(await page.textContent("#lidarr-status"), fill(w["setup.lidarr.ok"], { version: "3.2.1.5196" }));
  assert.deepEqual([await page.inputValue("#root"), await page.inputValue("#quality"), await page.inputValue("#metadata")],
    ["/music", "Lossless", "None"], "the root folder's quality profile, and only the asked album");
  await shot("3-choices");
  await page.click("#choice-save");
  await page.waitForSelector("#done:not([hidden])");
  assert.equal(await page.textContent("#title"), w["setup.heading.ready"]);
  await shot("4-ready");

  await page.click("#plex-start");
  await page.waitForSelector("#plex-pin:not([hidden])");
  assert.equal(await page.textContent("#plex-code"), "ABCD");
  const [tab] = await Promise.all([ctx.waitForEvent("page"), page.click("#plex-link")]);
  await tab.close();
  await page.waitForSelector("#plex-servers:not([hidden])");
  assert.equal(await page.inputValue("#plex-list input:checked"), s.plex, "the address that answers comes first");
  await shot("5-servers");
  await page.click("#plex-save");
  await page.waitForSelector("#plex-now:not([hidden])");
  assert.equal(await page.textContent("#plex-now"), fill(w["setup.plex.connected"], { url: s.plex }));
  await shot("6-plex");

  const [download] = await Promise.all([page.waitForEvent("download"), page.click("#backup-get")]);
  assert.match(download.suggestedFilename(), /^musaarr-backup-\d{4}-\d{2}-\d{2}\.zip$/);
  await page.setInputFiles("#restore-file", await download.path());
  await page.click("#restore-go");
  assert.equal(await page.textContent("#restore-status"), w["setup.backup.warn"], "the first press only warns");
  await page.click("#restore-go");
  await page.waitForSelector("#restore-status.is-ok");
  await page.waitForSelector("#restore-status:not(.is-ok)", { state: "attached" }); // the page reloads on the restored install
  await page.waitForSelector("#done:not([hidden])");
  assert.equal(await page.textContent("#plex-now"), fill(w["setup.plex.connected"], { url: s.plex }), "Plex came back");
  await shot("7-restored");

  await page.click("#done a");
  await page.waitForURL(s.base + "/");
  await page.waitForSelector("#settings:not([hidden])");
  assert.equal(await page.isVisible("#signout"), true);
  if (shots) await page.getByRole("button", { name: w["opening.skip"] }).click().catch(() => {});
  await shot("8-app");
  await ctx.close();
  assert.deepEqual(errors, []);
}

test("the first run, from the sign-up to the app", { timeout: 300_000 }, async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "musaarr-e2e-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const binary = join(dir, "musaarr.test");
  execFileSync("go", ["test", "-c", "-o", binary, "."], { cwd: root, stdio: "inherit" });
  const browser = await chromium.launch();
  t.after(() => browser.close());
  const runs = [
    ["en", { viewport: { width: 1440, height: 900 } }],
    ["fr", { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, colorScheme: "dark" }],
  ];
  for (const [i, [lang, device]] of runs.entries()) {
    await t.test(`${lang}, ${device.isMobile ? "phone" : "computer"}`, async () => {
      const s = await serve(binary, 18190 + i); // each run starts from an empty data folder
      try {
        await walk(browser, s, lang, device, process.env.E2E_SHOTS);
      } finally {
        await fetch(`${s.control}/stop`, { method: "POST" }).catch(() => {});
        s.proc.kill();
      }
    });
  }
});
