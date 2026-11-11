// 界面原型的「舞台」：把真实界面嵌进一个 iframe、按窗口大小缩放、让它走到某个场景。
// 评审用的目录页（index.html）和给访客看的导览页（tour.html）共用这一份，两张页面只是外面的样子不同。
//
// 切场景的办法：每个场景换一个新的 iframe，地址里带上 scene=场景编号。数据由管着仿真后端的那一方在页面加载时摆好
// （Node 版是 proto.js，静态版是页面里的 mock.js）；进页面后要自动点的那几下，等里面的播放器（player.js）说「我好了」再发给它。
// 场景清单、界面在哪，都从 window.MD_PROTO 读（Node 版是 /__proto/data.js，静态版是构建时写好的 data.js）。
(function () {
  "use strict";
  // 固定大小的窗口。另有两种跟着这块区域走的：fit 铺满；auto 铺满，但不小于程序的最小窗口（小了就整体缩小显示）
  var VIEWS = { def: [1280, 850], min: [1024, 576], wide: [1600, 900], phone: [390, 844] };
  var MIN = VIEWS.min, NARROW = 700; // 区域窄于 NARROW 就当是手机：界面按手机的版式铺满，不画窗口三键

  // o.stage  放界面的那块区域（量它的大小）    o.wrap  套在 iframe 外面的框（iframe 放在它里面）
  // o.pad    [左右, 上下] 区域里要留出的空白，也可以是返回它的函数（宽屏窄屏留的不一样时）    o.title  iframe 的名字（读屏用）
  // o.onStatus(st)  st.state：loading 正在加载 / playing 正在自动点（st.n、st.total）/ done 走完了 / fail 没走完（st.message）
  // o.onLayout(info)  info：{ w, h, k, fill }，界面的逻辑宽高、缩放比例、是不是铺满
  function MDStage(o) {
    var P = window.MD_PROTO || {};
    var padOf = function () { return (typeof o.pad === "function" ? o.pad() : o.pad) || [0, 0]; };
    var frame = null, cur = null, run = 0, view = "def", watchdog = 0;
    var status = function (st) { if (o.onStatus) o.onStatus(st); };
    var avail = function () { var pad = padOf(); return [Math.max(120, o.stage.clientWidth - pad[0]), Math.max(120, o.stage.clientHeight - pad[1])]; };
    var narrow = function () { return (view === "fit" || view === "auto") && avail()[0] < NARROW; };
    // 手机上是浏览器里打开的，没有窗口三键
    var hosted = function () { return view !== "phone" && !narrow(); };

    function url(sc, prefs, play) {
      var qs = [];
      if (play) qs.push("proto=1", "run=" + run);
      // 单独打开时，Node 版不带场景：那边只有一份后端，带上会把正嵌着的这一页的数据也重置掉
      if (play || P.static) qs.push("scene=" + encodeURIComponent(sc.id));
      if (hosted()) qs.push("host=1");
      if (prefs && prefs.theme) qs.push("theme=" + encodeURIComponent(prefs.theme));
      if (prefs && prefs.scheme) qs.push("scheme=" + encodeURIComponent(prefs.scheme));
      return (P.app || "app/index.html") + "?" + qs.join("&") + (sc.hash || "#/instances");
    }

    function layout() {
      if (!frame) return;
      var a = avail(), v = VIEWS[view], w, h, k, fill = !v;
      if (v) { w = v[0]; h = v[1]; k = Math.min(1, a[0] / w, a[1] / h); }
      else if (view === "auto" && !narrow()) { k = Math.min(1, a[0] / MIN[0], a[1] / MIN[1]); w = Math.round(a[0] / k); h = Math.round(a[1] / k); }
      else { k = 1; w = a[0]; h = a[1]; }
      frame.style.width = w + "px"; frame.style.height = h + "px";
      frame.style.transform = k < 1 ? "scale(" + k + ")" : "";
      o.wrap.style.width = Math.round(w * k) + "px"; o.wrap.style.height = Math.round(h * k) + "px";
      if (o.onLayout) o.onLayout({ w: w, h: h, k: k, fill: fill });
    }

    // 切到一个场景。opt：{ view, theme, scheme }
    function go(sc, opt) {
      opt = opt || {};
      cur = sc; run++; view = opt.view || "def";
      var my = run;
      // 每个场景换一个新的 iframe：旧页面里有没保存的修改时，直接改地址会弹「离开此网站？」
      var f = document.createElement("iframe");
      f.title = o.title || "MineDeck 界面";
      f.allow = "clipboard-write";
      f.src = url(sc, opt, true);
      o.wrap.replaceChildren(f);
      frame = f;
      layout();
      status({ state: "loading" });
      clearTimeout(watchdog);
      // 里面的播放器一直没说「我好了」（它在界面那一页的最前面，正常一两秒就到）：多半是页面没打开
      watchdog = setTimeout(function () { if (my === run) status({ state: "fail", message: "界面没有加载出来" }); }, 30000);
    }

    addEventListener("message", function (e) {
      var d = e.data;
      if (!frame || e.source !== frame.contentWindow || !d || d.mdProto !== 1 || d.run !== String(run)) return;
      var steps = (cur && cur.steps) || [];
      if (d.type === "ready") {
        clearTimeout(watchdog);
        frame.contentWindow.postMessage({ mdProto: 1, type: "play", run: String(run), steps: steps }, "*");
        status({ state: "playing", n: 0, total: steps.length });
      } else if (d.type === "step") status({ state: "playing", n: d.n, total: d.total });
      else if (d.type === "done") status({ state: "done" });
      else if (d.type === "fail") status({ state: "fail", message: d.message || "" });
    });
    addEventListener("resize", layout);

    return {
      go: go, layout: layout,
      // 在新标签页里单独打开当前这一页用的地址（不自动点）
      standalone: function (sc, prefs) { return url(sc, prefs, false); },
      narrow: narrow,
    };
  }

  // 状态写成一句话（中文）。返回 { text, warn }；走完了是空串
  MDStage.say = function (st) {
    if (st.state === "loading") return { text: "正在加载界面…", warn: false };
    if (st.state === "playing") return { text: st.total ? "正在自动走到这个状态（" + Math.max(1, st.n) + " / " + st.total + "）…" : "正在加载界面…", warn: false };
    if (st.state === "fail") return { text: "自动操作没走完：" + st.message + "。界面停在了半路，可以手动接着点。", warn: true };
    return { text: "", warn: false };
  };
  MDStage.VIEWS = VIEWS;
  window.MDStage = MDStage;
})();
