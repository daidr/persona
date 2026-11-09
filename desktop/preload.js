// Runs before the hub's web app in the desktop app's windows (desktop.go preload). System webviews have no web
// Notification API (WKWebView) or show it as the webview's own (WebKitGTK, WebView2): this puts the app's in its
// place, so the web app's notifications (web/src/lib/notify.ts postNotification) become the system's, with the same
// tag replacing, close() taking back and onclick opening the agent.
(() => {
  const cfg = __PERSONA_DESKTOP__;
  const bridge = window.mygo;
  if (!cfg.origin || location.origin !== cfg.origin || !bridge) return;

  let permission = cfg.permission;
  const shown = new Map();
  let seq = 0;

  class DesktopNotification extends EventTarget {
    static get permission() {
      return permission;
    }
    static async requestPermission(callback) {
      try {
        permission = await bridge.call("Persona.RequestPermission");
      } catch {
        permission = "denied";
      }
      if (typeof callback === "function") callback(permission);
      return permission;
    }
    // No buttons: the web app then posts a plain notification that opens the card.
    static get maxActions() {
      return 0;
    }

    constructor(title, options = {}) {
      super();
      this.title = String(title);
      this.body = options.body ? String(options.body) : "";
      this.tag = options.tag ? String(options.tag) : "";
      this.icon = options.icon || "";
      this.silent = Boolean(options.silent);
      this.data = options.data ?? null;
      this.onclick = this.onclose = this.onshow = this.onerror = null;
      this._id = this.tag || "n" + ++seq + "-" + Date.now();
      shown.get(this._id)?._fire("close");
      shown.set(this._id, this);
      bridge
        .call("Persona.Notify", { id: this._id, title: this.title, body: this.body, silent: this.silent })
        .then((p) => {
          if (p === "denied") {
            permission = "denied";
            this._drop();
            this._fire("error");
          } else this._fire("show");
        })
        .catch(() => {
          this._drop();
          this._fire("error");
        });
    }

    close() {
      if (shown.get(this._id) !== this) return;
      this._drop();
      bridge.call("Persona.Close", this._id).catch(() => {});
      this._fire("close");
    }

    _drop() {
      if (shown.get(this._id) === this) shown.delete(this._id);
    }

    _fire(type) {
      const event = new Event(type);
      this.dispatchEvent(event);
      const handler = this["on" + type];
      if (typeof handler === "function") handler.call(this, event);
    }
  }

  bridge.on("personaNotificationClick", (id) => {
    const n = shown.get(id);
    if (!n) return;
    n._fire("click");
    if (shown.get(id) === n) {
      n._drop();
      n._fire("close");
    }
  });

  Object.defineProperty(window, "Notification", { value: DesktopNotification, configurable: true, writable: true });
})();
