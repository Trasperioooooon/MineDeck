// 界面原型的「播放器」：跑在嵌着的界面那一页里（Node 版由 proto.js 塞进 <head>，静态版拼在 mock.js 末尾）。
// 两件事，都看地址里的参数：
//   host=1   假装在原生窗口里：界面只要看到 hostWinClose 这几个函数就会画出窗口三键（见 app.js 的 initHostChrome）
//   proto=1  这一页是嵌在目录页或导览页里的：告诉外面「我好了」，外面把这个场景要点的那几下发过来，在这里依次执行，
//            走到第几步、走没走完再报回去。
// 为什么不让外面的页面直接伸手进来点：双击打开（file://）时，浏览器把每个文件都当成不同的来源，
// 外面的页面摸不到这一页里的东西，只能互相发消息（postMessage）。Node 版和静态版都走这一套，不分两种写法。
(function () {
  "use strict";
  var q = new URLSearchParams(location.search);

  if (q.get("host") === "1") {
    var n = function () {};
    window.hostWinClose = n; window.hostWinMin = n; window.hostWinMaxToggle = n; window.hostWinDrag = n;
    window.hostCloseAck = n; window.hostWinHide = n; window.hostWinQuit = n;
    window.hostWinIsMax = function () { return Promise.resolve(false); };
    window.hostDblClickTime = function () { return Promise.resolve(500); };
  }

  if (q.get("proto") !== "1" || window.parent === window) return;
  var run = q.get("run") || "";
  var tell = function (type, more) {
    var msg = { mdProto: 1, type: type, run: run };
    for (var k in more || {}) msg[k] = more[k];
    window.parent.postMessage(msg, "*"); // file:// 下来源是 "null"，只能写 "*"；消息里没有要保密的东西
  };
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };

  async function waitFor(sel, ms) {
    var end = Date.now() + (ms || 6000);
    for (;;) {
      var el = null;
      try { el = document.querySelector(sel); } catch (e) {}
      if (el) return el;
      if (Date.now() > end) throw new Error("没等到 " + sel);
      await sleep(60);
    }
  }

  // 场景里写的动作，见 scenes.js 开头的说明。每步都先等元素出现
  async function play(steps) {
    await waitFor("#main > *", 45000); // 界面第一次画出来。放在网站上、网又慢的时候，光是把界面下载下来就可能要十几秒
    await sleep(350);
    for (var i = 0; i < steps.length; i++) {
      var st = steps[i];
      tell("step", { n: i + 1, total: steps.length });
      if (st.sleep) { await sleep(st.sleep); continue; }
      if (st.key) { document.dispatchEvent(new KeyboardEvent("keydown", { key: st.key, bubbles: true })); await sleep(120); continue; }
      var sel = st.click || st.dblclick || st.fill || st.select || st.wait || st.scroll || st.upload;
      var el = await waitFor(sel);
      if (st.click) { el.scrollIntoView({ block: "nearest" }); el.click(); }
      else if (st.dblclick) el.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
      else if (st.fill) { el.focus(); el.value = st.value; el.dispatchEvent(new Event("input", { bubbles: true })); el.dispatchEvent(new Event("change", { bubbles: true })); }
      else if (st.select) { el.value = st.value; el.dispatchEvent(new Event("change", { bubbles: true })); }
      else if (st.scroll) el.scrollTop = st.to === "bottom" ? el.scrollHeight : 0;
      else if (st.upload) {
        // 假文件：jar 以 PK 开头（仿真后端靠它认是不是 zip），内容里带上名字，免得几个文件哈希相同被当成同一个
        var dt = new DataTransfer();
        st.files.forEach(function (f) {
          dt.items.add(new File([f.text !== undefined ? f.text : "PK\u0003\u0004 " + f.name + " ".repeat(f.kb ? f.kb * 1024 : 0)], f.name));
        });
        el.files = dt.files;
        el.dispatchEvent(new Event("change", { bubbles: true }));
        await sleep(900);
      }
      await sleep(st.click || st.select ? 260 : 120);
    }
  }

  var started = false;
  addEventListener("message", function (e) {
    var d = e.data;
    if (e.source !== window.parent || !d || d.mdProto !== 1 || d.type !== "play" || d.run !== run || started) return;
    started = true;
    play(d.steps || []).then(function () { tell("done"); }, function (err) { tell("fail", { message: String(err && err.message || err) }); });
  });
  tell("ready");
})();
