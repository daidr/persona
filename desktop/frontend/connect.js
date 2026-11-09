// The connect page: the hubs this app opens, adding one, and the app's settings. Everything goes through the
// Connect service of the app (bridge.go); the texts come from it too, in the user's language.
(() => {
  const call = (method, ...args) => window.mygo.call("Connect." + method, ...args);
  const $ = (id) => document.getElementById(id);
  const params = new URLSearchParams(location.search);
  let state = null;
  let editing = "";
  let confirming = "";

  const t = (key, vars = {}) => {
    let s = state?.strings?.[key] ?? key;
    for (const [k, v] of Object.entries(vars)) s = s.split("{" + k + "}").join(v);
    return s;
  };

  function el(tag, props = {}, ...children) {
    const node = document.createElement(tag);
    Object.assign(node, props);
    for (const c of children) node.append(c);
    return node;
  }

  function texts() {
    document.documentElement.lang = state.lang;
    document.querySelectorAll("[data-t]").forEach((n) => (n.textContent = t(n.dataset.t)));
    document.querySelectorAll("[data-ph]").forEach((n) => (n.placeholder = t(n.dataset.ph)));
    $("shortcut-label").textContent = t("settingShortcut", { shortcut: state.shortcut });
    $("version").textContent = t("versionLine", { version: state.version });
  }

  function offline() {
    const hub = params.get("offline");
    if (!hub) return;
    const known = state.hubs.find((h) => h.url === hub);
    $("offline").hidden = false;
    $("offline-title").textContent = t("offlineTitle", { hub: known?.title || hub });
    $("offline-detail").textContent = params.get("detail") || "";
    $("offline-retry").onclick = () => call("Open", hub).catch(show);
  }

  function hubRow(h) {
    const actions = [];
    let main;
    if (editing === h.url) {
      const input = el("input", { type: "text", value: h.title === h.address || !h.address ? "" : h.title, placeholder: t("addLabelPlaceholder") });
      const save = () => call("Rename", h.url, input.value).then(() => ((editing = ""), load()), show);
      input.onkeydown = (e) => {
        if (e.key === "Enter") save();
        if (e.key === "Escape") (editing = ""), render();
      };
      setTimeout(() => input.focus());
      main = input;
      actions.push(el("button", { className: "primary", textContent: t("save"), onclick: save }));
      actions.push(el("button", { textContent: t("cancel"), onclick: () => ((editing = ""), render()) }));
    } else {
      main = el("button", { className: "open", title: t("openHub"), onclick: () => call("Open", h.url).catch(show) }, el("span", { className: "name", textContent: h.title }), el("span", { className: "addr", textContent: h.address || "" }));
      if (h.current) actions.push(el("span", { className: "chip current", textContent: t("currentHub") }));
      if (h.pinned) actions.push(el("span", { className: "chip", title: t("pinnedHint"), textContent: t("pinned") }));
      actions.push(el("button", { className: "link", textContent: t("rename"), onclick: () => ((editing = h.url), (confirming = ""), render()) }));
      if (confirming === h.url) {
        actions.push(el("button", { className: "link danger", textContent: t("removeConfirm"), onclick: () => call("Remove", h.url).then(() => ((confirming = ""), load()), show) }));
      } else {
        actions.push(el("button", { className: "link danger", textContent: t("remove"), onclick: () => ((confirming = h.url), render()) }));
      }
    }
    return el("li", { className: "hub" }, main, ...actions);
  }

  function render() {
    const list = $("hubs");
    list.replaceChildren(...state.hubs.map(hubRow));
    $("hubs-empty").hidden = state.hubs.length > 0;
    $("login").checked = state.openAtLogin;
    $("shortcut").checked = state.shortcutOn;
  }

  function show(err) {
    const box = $("add-error");
    box.textContent = String(err?.message || err || "");
    box.hidden = !box.textContent;
  }

  async function load() {
    state = await call("State");
    texts();
    offline();
    render();
  }

  $("add").onsubmit = async (e) => {
    e.preventDefault();
    show("");
    try {
      const message = await call("Add", $("add-input").value, $("add-label").value);
      if (message) show(message);
      else $("add-input").value = $("add-label").value = "";
    } catch (err) {
      show(err);
    }
  };
  $("login").onchange = (e) => call("SetOpenAtLogin", e.target.checked).then(load, show);
  $("shortcut").onchange = (e) => call("SetShortcut", e.target.checked).then(load, show);

  load().catch(show);
})();
