// 静态版界面原型的运行时。构建脚本（../proto-static.js）把它和仿真后端的源码拼成一份 mock.js，塞进界面那一页的 <head>，
// 这样界面不用任何服务器就能跑：双击打开（file://）可以，丢到随便哪个静态托管上也可以。它做三件事：
//
//   1. 给仿真后端用到的 Node 模块做最小的替身：http、fs、path、vm、url、crypto，外加 Buffer。
//      server.js 和 proto.js 的源码原样打进来，一行不改，Node 下的交互测试和原型照旧。
//   2. 接管这一页的 fetch 和 EventSource：界面发给 /api/… 的请求不出浏览器，直接交给仿真后端的处理函数；
//      发往别的网站的请求一律拒掉。演示不联网。
//   3. 页面一加载就按地址里的 scene 参数把数据摆成那个场景的样子（同 Node 版 proto.js 做的事）。
//
// 这个文件不单独运行。它前面由构建脚本放好三样东西：
//   __MODULES  { "/tools/ui-dev/server.js": function (module, exports, require, __dirname, __filename, Buffer, process) { 源码 }, … }
//   __FILES    后端启动时要读的文件，{ 路径: 内容 }：app.go 里写版本号的那一行，和一个空的 props-catalog.js（内容走 __PROPS，只要「有这个文件」）
//   __PROPS    界面的 props-catalog.js 里每一项的键和默认值（构建时在 Node 里求好的，这里就不用 eval 了）
//   __PRELOAD  界面首页原来要预加载的字体（构建时从首页里拿掉了，原因见 proto-static.js 里 PRELOAD 的说明）
// 以及入口模块的路径 __ENTRY。

// 预加载字体：放在网站上时照原样补回去，早点开始下载；双击打开（file://）时不补，浏览器会拦并在控制台报错
if (location.protocol !== "file:") for (const f of __PRELOAD) {
  if (/^([a-z][a-z0-9+.-]*:|\/)/i.test(f.href)) continue; // 只补界面自己文件夹里的；写成外站或根路径的不是演示的东西
  const l = document.createElement("link");
  l.rel = "preload"; l.href = f.href;
  if (f.as) l.as = f.as;
  if (f.type) l.type = f.type;
  if (f.cors) l.crossOrigin = "anonymous";
  document.head.appendChild(l);
}

// 最后一道保险：告诉浏览器这一页只许读自己文件夹里的东西。下面接管了 fetch 和 EventSource，仿真数据里也没有外站的地址，
// 正常用不上它；但哪天界面里多了一张外站的图、一段外站的脚本，浏览器会直接拦下并在控制台里说明，而不是悄悄连出去。
// （点链接跳到外站不归它管：那几个链接是要人自己点的，在新标签页里打开。）
{
  const csp = document.createElement("meta");
  csp.httpEquiv = "Content-Security-Policy";
  csp.content = "default-src 'self' data: blob: 'unsafe-inline'" + (location.protocol === "file:" ? " file:" : "");
  document.head.appendChild(csp);
}

// ==================== Node 模块的替身 ====================

const utf8 = new TextEncoder(), fromUtf8 = new TextDecoder("utf-8");

// Buffer：仿真后端只在读上传的文件时用到（拼分段、找分隔线、切片、转字符串）
class Buf extends Uint8Array {
  static from(x, encoding) {
    if (typeof x === "string") {
      if (encoding === "latin1" || encoding === "binary") { const b = new Buf(x.length); for (let i = 0; i < x.length; i++) b[i] = x.charCodeAt(i) & 255; return b; }
      const u = utf8.encode(x);
      return new Buf(u.buffer, u.byteOffset, u.byteLength);
    }
    if (x instanceof ArrayBuffer) return new Buf(x);
    if (ArrayBuffer.isView(x)) return new Buf(x.buffer, x.byteOffset, x.byteLength);
    return super.from(x);
  }
  static concat(list) {
    let n = 0;
    for (const c of list) n += c.length;
    const out = new Buf(n);
    let at = 0;
    for (const c of list) { out.set(c, at); at += c.length; }
    return out;
  }
  toString(encoding) {
    if (encoding === "latin1" || encoding === "binary") {
      let s = "";
      for (let i = 0; i < this.length; i += 8192) s += String.fromCharCode.apply(null, this.subarray(i, i + 8192));
      return s;
    }
    return fromUtf8.decode(this);
  }
  indexOf(needle, from = 0) {
    if (typeof needle === "number") return super.indexOf(needle, from);
    if (typeof needle === "string") needle = Buf.from(needle);
    if (!needle.length) return from;
    for (let i = super.indexOf(needle[0], from); i >= 0 && i + needle.length <= this.length; i = super.indexOf(needle[0], i + 1)) {
      let k = 1;
      while (k < needle.length && this[i + k] === needle[k]) k++;
      if (k === needle.length) return i;
    }
    return -1;
  }
}

// crypto 只用到一处：给「从电脑添加」的文件算 SHA-1 当编号。浏览器自带的 crypto.subtle 是异步的，
// 在不是 https 的页面上还没有，所以自己算一遍（同步，够用）
function sha1hex(bytes) {
  let h0 = 0x67452301, h1 = 0xEFCDAB89, h2 = 0x98BADCFE, h3 = 0x10325476, h4 = 0xC3D2E1F0;
  const w = new Int32Array(80), n = bytes.length, full = n - (n % 64);
  const tail = new Uint8Array(n - full < 56 ? 64 : 128); // 最后不满一块的部分，补上结束标记和总长度
  tail.set(bytes.subarray(full));
  tail[n - full] = 0x80;
  const dv = new DataView(tail.buffer);
  dv.setUint32(tail.length - 8, Math.floor(n / 0x20000000));
  dv.setUint32(tail.length - 4, (n << 3) >>> 0);
  const block = (b, o) => {
    for (let i = 0; i < 16; i++, o += 4) w[i] = b[o] << 24 | b[o + 1] << 16 | b[o + 2] << 8 | b[o + 3];
    for (let i = 16; i < 80; i++) { const t = w[i - 3] ^ w[i - 8] ^ w[i - 14] ^ w[i - 16]; w[i] = t << 1 | t >>> 31; }
    let a = h0, bb = h1, c = h2, d = h3, e = h4;
    for (let i = 0; i < 80; i++) {
      const f = i < 20 ? (bb & c | ~bb & d) + 0x5A827999 : i < 40 ? (bb ^ c ^ d) + 0x6ED9EBA1 : i < 60 ? (bb & c | bb & d | c & d) + 0x8F1BBCDC : (bb ^ c ^ d) + 0xCA62C1D6;
      const t = (a << 5 | a >>> 27) + f + e + w[i] | 0;
      e = d; d = c; c = bb << 30 | bb >>> 2; bb = a; a = t;
    }
    h0 = h0 + a | 0; h1 = h1 + bb | 0; h2 = h2 + c | 0; h3 = h3 + d | 0; h4 = h4 + e | 0;
  };
  for (let o = 0; o < full; o += 64) block(bytes, o);
  for (let o = 0; o < tail.length; o += 64) block(tail, o);
  return [h0, h1, h2, h3, h4].map(x => (x >>> 0).toString(16).padStart(8, "0")).join("");
}
const cryptoShim = {
  createHash(algo) {
    if (algo !== "sha1") throw new Error("静态版只会算 sha1，不会 " + algo);
    const parts = [];
    const h = { update(d) { parts.push(typeof d === "string" ? Buf.from(d) : d); return h; }, digest(enc) { const x = sha1hex(parts.length === 1 ? parts[0] : Buf.concat(parts)); if (enc !== "hex") throw new Error("静态版只出 hex"); return x; } };
    return h;
  },
};

// path：模块里的路径一律当成从仓库根算起的 "/tools/ui-dev/…"，正斜杠
const norm = p => {
  const abs = /^[\\/]/.test(p), out = [];
  for (const s of p.split(/[\\/]+/)) { if (!s || s === ".") continue; if (s === "..") out.pop(); else out.push(s); }
  return (abs ? "/" : "") + out.join("/") || (abs ? "/" : ".");
};
const pathShim = {
  sep: "/",
  normalize: norm,
  join: (...a) => norm(a.join("/")),
  resolve: (...a) => { let r = "/"; for (const s of a) r = /^[\\/]/.test(s) ? s : r + "/" + s; return norm(r); },
  extname: p => (/\.[^./\\]*$/.exec(p.split(/[\\/]/).pop()) || [""])[0],
};

// fs：静态版里界面的文件由浏览器自己从文件夹里读，后端只在启动时读两样，构建时已经放进 __FILES 了
const fsShim = {
  readFileSync(p, encoding) {
    const k = norm(String(p));
    if (!(k in __FILES)) throw Object.assign(new Error("静态版里没有这个文件：" + k), { code: "ENOENT" });
    return encoding ? __FILES[k] : Buf.from(__FILES[k]);
  },
  existsSync: p => norm(String(p)) in __FILES,
  statSync(p) {
    const k = norm(String(p));
    if (!(k in __FILES)) throw Object.assign(new Error("静态版里没有这个文件：" + k), { code: "ENOENT" });
    return { isFile: () => true, isDirectory: () => false };
  },
};

// vm：只有一处用，就是把 props-catalog.js 求值、取每一项的键和默认值。构建时在 Node 里求过了，这里直接给结果
const vmShim = { runInNewContext(code, ctx) { ctx.window.PROPS_CATALOG = __PROPS; } };

// http：没有真的端口可听。createServer 只是把处理函数记下来，请求由下面的 dispatch 直接递给它
let listener = null;
const httpShim = {
  createServer(...args) {
    listener = args.find(a => typeof a === "function") || null;
    const srv = {
      once: () => srv, on: () => srv,
      listen(...args) { const cb = args.find(a => typeof a === "function"); if (cb) setTimeout(cb, 0); return srv; },
      address: () => ({ address: "127.0.0.1", port: 27333 }), // 界面「远程访问」里显示的端口：MineDeck 默认的那个
      closeAllConnections() {},
      close(cb) { listener = null; if (cb) setTimeout(cb, 0); },
    };
    return srv;
  },
};

const BUILTIN = { http: httpShim, fs: fsShim, path: pathShim, vm: vmShim, url: { URL }, crypto: cryptoShim };
const PROCESS = { argv: [], platform: "browser", env: {} };
const loaded = {};
function load(id) {
  if (loaded[id]) return loaded[id].exports;
  if (!__MODULES[id]) throw new Error("静态版里没有打进这个模块：" + id);
  const module = loaded[id] = { exports: {} }, dir = id.slice(0, id.lastIndexOf("/")) || "/";
  const require = name => {
    if (name in BUILTIN) return BUILTIN[name];
    if (!/^\.\.?\//.test(name)) throw new Error("静态版里用不了 Node 模块 " + name);
    const p = norm(dir + "/" + name);
    return load(p in __MODULES ? p : p + ".js");
  };
  __MODULES[id].call(module.exports, module, module.exports, require, dir, id, Buf, PROCESS);
  return module.exports;
}

// ==================== 启动仿真后端，摆好场景 ====================

const query = new URLSearchParams(location.search);
const ready = new Promise(r => r(load(__ENTRY))).then(async proto => {
  const m = await proto.startProto({ port: 0 });
  proto.applyScene(m.state, query.get("scene") || "");
  return m;
});
ready.catch(e => console.error("[MineDeck 演示] 仿真后端没有起来：", e));

// ==================== 把请求递给仿真后端 ====================

// 假的 req / res。sink 收响应：head(状态码, 响应头)、data(一块内容)、end()。返回 { close() }，客户端断开时调
function dispatch(method, url, headers, getBody, sink) {
  const on = { data: [], end: [], close: [] };
  let pumped = false, gone = false, ended = false, headSent = false, status = 200, hdrs = {};
  // 请求体等有人来读（挂上 data / end）才给：处理函数在读之前可能先 await 了别的，定时发会错过
  const pump = async () => {
    let body = null;
    try { body = await getBody(); } catch (e) { console.error("[MineDeck 演示] 请求体没读出来：", e); }
    if (gone) return;
    if (body && body.length) for (const fn of on.data.slice()) fn(body);
    for (const fn of on.end.slice()) fn();
  };
  // 连接没了：客户端断开，或者响应发完了。同 Node，两种情况都会让挂在 req 上的 close 触发一次
  const close = () => { if (gone) return; gone = true; for (const fn of on.close.slice()) fn(); };
  const req = {
    method, url, headers,
    on(ev, fn) {
      (on[ev] || (on[ev] = [])).push(fn);
      if ((ev === "data" || ev === "end") && !pumped) { pumped = true; Promise.resolve().then(pump); }
      return req;
    },
  };
  req.once = req.on; // 这里的 data、end、close 本来就各只来一次
  const flush = () => { if (!headSent) { headSent = true; sink.head(status, hdrs); } };
  const res = {
    writeHead(code, h) { status = code; hdrs = Object.assign({}, hdrs, h); return res; },
    setHeader(k, v) { hdrs[k] = v; },
    write(chunk) { if (gone || ended) return false; flush(); sink.data(chunk); return true; },
    end(chunk) {
      if (gone || ended) return res;
      flush();
      if (chunk != null) sink.data(chunk);
      ended = true;
      sink.end();
      Promise.resolve().then(close);
      return res;
    },
  };
  ready.then(() => {
    if (!listener) throw new Error("仿真后端已经关了");
    return listener(req, res);
  }).catch(e => {
    console.error("[MineDeck 演示]", e);
    if (headSent) return;
    res.writeHead(500, { "Content-Type": "application/json; charset=utf-8" });
    res.end(JSON.stringify({ ok: false, error: "演示出错了：" + (e && e.message || e) }));
  });
  return { close };
}

// 这个地址是不是发给仿真后端的。是就返回 "/api/…?…"，不是返回 null。
// 界面里写的都是 "/api/…" 这种根路径，不能拿去对着页面地址解析：file:// 下会变成 file:///E:/api/…，
// 放在网站的子目录下又会指到站点根上去。所以直接看写的是什么
const OURS = /^\/(api|bg|__mock|__proto)(\/|\?|$)/;
function routeOf(raw) {
  if (/^\/(?!\/)/.test(raw)) return OURS.test(raw) ? raw : null;
  try {
    const u = new URL(raw, location.href), p = u.pathname.replace(/^\/[A-Za-z]:(?=\/)/, "");
    return (u.protocol === "file:" || u.origin === location.origin) && OURS.test(p) ? p + u.search : null;
  } catch { return null; }
}
// 不是发给仿真后端的请求，只放行界面自己文件夹里的静态文件。外站的不去；写成 /xxx 这种根路径的也不去：
// 放在网站的子目录下时，它指的是子目录外面，不是演示自己的东西
const HOME = new URL(".", location.href).href;
const isOwnFile = raw => { try { return new URL(raw, location.href).href.startsWith(HOME); } catch { return false; } };

// ---- fetch ----
const WHOLE = 32 * 1048576, HEAD = 65536;
// 上传的文件太大就只留开头一小段：演示里没有地方真的存它，整个读进内存只会把页面卡住
function slim(fd) {
  const out = new FormData();
  for (const [k, v] of fd.entries()) out.append(k, typeof v === "string" || v.size <= WHOLE ? v : new File([v.slice(0, HEAD)], v.name, { type: v.type }));
  return out;
}
const aborted = signal => signal.reason instanceof Error ? signal.reason : new DOMException("The operation was aborted.", "AbortError");
const realFetch = window.fetch ? window.fetch.bind(window) : null;

window.fetch = function (input, init) {
  init = init || {};
  const isReq = typeof Request !== "undefined" && input instanceof Request;
  const raw = isReq ? input.url : String(input);
  const route = routeOf(raw);
  if (route == null) {
    if (!realFetch || !isOwnFile(raw)) return Promise.reject(new TypeError("这是不联网的演示，没有去请求 " + raw));
    return realFetch(input, init);
  }
  const method = String(init.method || (isReq ? input.method : "GET")).toUpperCase();
  const headers = {};
  new Headers(init.headers || (isReq ? input.headers : undefined)).forEach((v, k) => { headers[k.toLowerCase()] = v; });
  let body = init.body, getBody = () => null;
  if (body == null) { if (isReq && method !== "GET" && method !== "HEAD") getBody = () => input.arrayBuffer().then(b => Buf.from(b)); }
  else if (typeof body === "string") getBody = () => Buf.from(body);
  else {
    if (typeof FormData !== "undefined" && body instanceof FormData) body = slim(body);
    // 借 Response 把 FormData、Blob 这些编成字节。分段的分隔线在这一刻就定了，写在它的 content-type 里
    const carrier = new Response(body), ct = carrier.headers.get("content-type");
    if (ct && !headers["content-type"]) headers["content-type"] = ct;
    getBody = () => carrier.arrayBuffer().then(b => Buf.from(b));
  }
  let bodyOnce = null;
  const signal = init.signal || (isReq ? input.signal : null);
  return new Promise((resolve, reject) => {
    if (signal && signal.aborted) return reject(aborted(signal));
    const chunks = [];
    let status = 200, hdrs = {}, settled = false;
    const h = dispatch(method, route, headers, () => bodyOnce || (bodyOnce = Promise.resolve(getBody())), {
      head(s, hh) { status = s; hdrs = hh; },
      data(c) { chunks.push(c); },
      // 真的请求不会在发出去的同一刻就回来：留到下一轮再交回去
      end() { setTimeout(() => { if (settled) return; settled = true; try { resolve(new Response([101, 204, 205, 304].includes(status) ? null : new Blob(chunks), { status, headers: hdrs })); } catch (e) { reject(e); } }, 0); },
    });
    if (signal) signal.addEventListener("abort", () => { if (settled) return; settled = true; h.close(); reject(aborted(signal)); }, { once: true });
  });
};

// ---- EventSource（控制台、隧道日志、AI 助手、MCP 的待确认提案）----
class DemoEventSource extends EventTarget {
  constructor(url) {
    super();
    this.url = String(url); this.withCredentials = false; this.readyState = 0;
    this.onopen = this.onmessage = this.onerror = null;
    this._conn = null; this._retry = 0; this._queue = []; this._timer = 0;
    this._route = routeOf(this.url);
    // 界面是先 new 再挂 onmessage 的，所以什么都得等这一轮过去再发
    if (this._route == null) this._later(() => { this.readyState = 2; this._fire("error"); });
    else this._later(() => this._connect());
  }
  _later(fn) {
    this._queue.push(fn);
    if (this._timer) return;
    this._timer = setTimeout(() => { this._timer = 0; const q = this._queue; this._queue = []; for (const f of q) f(); }, 0);
  }
  _fire(type, data) {
    const ev = type === "message" ? new MessageEvent("message", { data }) : new Event(type);
    this.dispatchEvent(ev);
    const fn = this["on" + type];
    // 处理函数里抛的错照样报出来（控制台里看得到），但不能把这条流带崩
    if (typeof fn === "function") try { fn.call(this, ev); } catch (e) { setTimeout(() => { throw e; }, 0); }
  }
  _connect() {
    if (this.readyState === 2) return;
    let stream = false, buf = "";
    this._conn = dispatch("GET", this._route, { accept: "text/event-stream" }, () => null, {
      head: (s, hh) => {
        stream = s === 200 && /text\/event-stream/.test(String(hh["Content-Type"] || hh["content-type"] || ""));
        if (stream) this._later(() => { if (this.readyState !== 2) { this.readyState = 1; this._fire("open"); } });
      },
      data: c => {
        if (!stream) return;
        buf += typeof c === "string" ? c : c.toString();
        for (let at; (at = buf.indexOf("\n\n")) >= 0;) {
          const lines = buf.slice(0, at).split("\n").filter(l => l.startsWith("data:")).map(l => l.slice(5).replace(/^ /, ""));
          buf = buf.slice(at + 2);
          if (lines.length) { const data = lines.join("\n"); this._later(() => { if (this.readyState === 1) this._fire("message", data); }); }
        }
      },
      end: () => this._later(() => {
        if (this.readyState === 2) return;
        // 被拒绝（不是 200 的事件流）就此关掉；好好的流断了，同真的 EventSource：报一次错，过 3 秒重连
        if (!stream) { this.readyState = 2; this._fire("error"); return; }
        this.readyState = 0; this._fire("error");
        this._retry = setTimeout(() => this._connect(), 3000);
      }),
    });
  }
  close() {
    this.readyState = 2;
    clearTimeout(this._retry);
    clearTimeout(this._timer); this._timer = 0; this._queue = []; // 排着队还没发出去的事件，关了就不发了
    if (this._conn) this._conn.close();
  }
}
for (const [k, v] of [["CONNECTING", 0], ["OPEN", 1], ["CLOSED", 2]]) { DemoEventSource[k] = v; DemoEventSource.prototype[k] = v; }
window.EventSource = DemoEventSource;

// 别的出网办法界面现在都没用。先堵上，以后哪天用了，演示里会明明白白地报错，而不是悄悄连出去
window.WebSocket = function () { throw new Error("这是不联网的演示，用不了 WebSocket"); };
if (navigator.sendBeacon) navigator.sendBeacon = () => false;
