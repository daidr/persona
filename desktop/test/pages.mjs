// Checks the desktop app's own pages in Chromium with a stand-in for MyGo's bridge (window.mygo): the connect page
// (texts, the hub list, adding, the offline notice) and preload.js's Notification bridge. Screenshots go to the
// directory given as the first argument, light and dark.
//
//   node desktop/test/pages.mjs /tmp/desktop-shots
import { createRequire } from "node:module";
import { readFileSync, mkdirSync } from "node:fs";
import { createServer } from "node:http";
import { extname, join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

// require(), not import: it finds a Playwright installed globally as well as one beside the repository.
const { chromium } = createRequire(import.meta.url)("playwright");
const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const out = process.argv[2] || "desktop-shots";
mkdirSync(out, { recursive: true });
const strings = JSON.parse(readFileSync(join(root, "internal/l10n/locales/en.json"), "utf8"));
const types = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".png": "image/png" };
const server = createServer((req, res) => {
  const path = new URL(req.url, "http://x").pathname;
  if (path === "/hub/") return res.end("<!doctype html><title>(2) Persona</title><body>hub</body>");
  try {
    const file = join(root, "frontend", path === "/" ? "index.html" : path);
    res.setHeader("content-type", types[extname(file)] || "application/octet-stream");
    res.end(readFileSync(file));
  } catch {
    res.statusCode = 404;
    res.end();
  }
}).listen(0);
const base = `http://127.0.0.1:${server.address().port}`;

const fail = (msg) => {
  console.error("FAIL", msg);
  process.exitCode = 1;
};

// The connect page's half of the bridge (bridge.go Connect).
const connectMock = (strings) => {
  const hubs = [
    { url: "https://hub.example.com:18662", title: "Home", address: "https://hub.example.com:18662", pinned: true, current: true },
    { url: "https://proxy.example.com/p", title: "proxy.example.com/p", address: "", pinned: false, current: false },
  ];
  window.calls = [];
  window.mygo = {
    on() {},
    async call(method, ...args) {
      window.calls.push([method, ...args]);
      switch (method) {
        case "Connect.State":
          return { lang: "en", strings, hubs, current: hubs[0].url, openAtLogin: false, shortcut: "Ctrl+Alt+P", shortcutOn: true, version: "0.1.0" };
        case "Connect.Add":
          return args[0] ? "" : strings.errorEmpty;
        default:
          return null;
      }
    },
  };
};

const browser = await chromium.launch();
for (const scheme of ["light", "dark"]) {
  const page = await browser.newPage({ colorScheme: scheme, viewport: { width: 720, height: 900 }, deviceScaleFactor: 1 });
  await page.addInitScript(connectMock, strings);
  await page.goto(`${base}/?offline=${encodeURIComponent("https://hub.example.com:18662")}&detail=Could%20not%20connect`);
  await page.waitForSelector(".hub");
  if ((await page.locator(".hub").count()) !== 2) fail("two hubs listed");
  if (!(await page.textContent("#offline-title")).includes("Home")) fail("offline title names the hub");
  if ((await page.textContent("h1")) !== "Persona") fail("title text");
  await page.click("#add-submit");
  if ((await page.textContent("#add-error")) !== strings.errorEmpty) fail("empty add shows the error");
  await page.fill("#add-input", "hub.example.com");
  await page.click("#add-submit");
  const calls = await page.evaluate(() => window.calls);
  if (!calls.some((c) => c[0] === "Connect.Add" && c[1] === "hub.example.com")) fail("add calls Connect.Add");
  await page.locator(".hub").nth(1).getByText(strings.remove).click();
  if (!(await page.getByText(strings.removeConfirm).isVisible())) fail("remove asks to confirm");
  await page.screenshot({ path: join(out, `connect-${scheme}.png`), fullPage: true });
  await page.close();
}

// preload.js: the web app's Notification calls become the bridge's.
const preload = readFileSync(join(root, "preload.js"), "utf8").replace("__PERSONA_DESKTOP__", JSON.stringify({ origin: base, permission: "granted" }));
const page = await browser.newPage();
await page.addInitScript(() => {
  const listeners = {};
  window.calls = [];
  window.mygo = {
    on: (name, fn) => (listeners[name] = fn),
    emit: (name, v) => listeners[name]?.(v),
    async call(method, ...args) {
      window.calls.push([method, ...args]);
      return method === "Persona.RequestPermission" ? "granted" : "granted";
    },
  };
});
await page.addInitScript(preload);
await page.goto(`${base}/hub/`);
const result = await page.evaluate(async () => {
  const log = [];
  const perm = await Notification.requestPermission();
  const a = new Notification("Build finished", { body: "All green", tag: "s1#finished" });
  a.onclick = () => log.push("click a");
  a.onclose = () => log.push("close a");
  const b = new Notification("Build finished again", { tag: "s1#finished" }); // same tag replaces a
  b.onclick = () => log.push("click b");
  await new Promise((r) => setTimeout(r, 20));
  window.mygo.emit("personaNotificationClick", "s1#finished");
  const c = new Notification("Needs you", { tag: "s2#needs" });
  c.close();
  await new Promise((r) => setTimeout(r, 20));
  return { perm, permission: Notification.permission, log, calls: window.calls, maxActions: Notification.maxActions };
});
if (result.perm !== "granted" || result.permission !== "granted") fail("permission " + JSON.stringify(result));
if (result.maxActions !== 0) fail("maxActions");
if (JSON.stringify(result.log) !== JSON.stringify(["close a", "click b"])) fail("events " + JSON.stringify(result.log));
const notifies = result.calls.filter((c) => c[0] === "Persona.Notify");
if (notifies.length !== 3 || notifies[0][1].id !== "s1#finished" || notifies[0][1].body !== "All green") fail("notify calls " + JSON.stringify(notifies));
if (!result.calls.some((c) => c[0] === "Persona.Close" && c[1] === "s2#needs")) fail("close calls Persona.Close");

// On a page of another origin preload.js leaves the browser's Notification alone.
const other = await browser.newPage();
await other.addInitScript(() => (window.mygo = { on() {}, call: async () => "granted" }));
await other.addInitScript(preload.replace(base, "https://hub.example.com"));
await other.goto(`${base}/hub/`);
if ((await other.evaluate(() => Notification.maxActions)) === 0) fail("another origin got the bridge");

await browser.close();
server.close();
console.log(process.exitCode ? "desktop pages: FAILED" : `desktop pages: OK (screenshots in ${out})`);
