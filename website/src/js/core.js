/* MineDeck 官网全站脚本。全部是增强：关掉 JS 时阅读、下载、导航都不受影响。 */
(function () {
  var doc = document.documentElement;
  doc.classList.add('js');
  var still = window.matchMedia && matchMedia('(prefers-reduced-motion: reduce)').matches;
  var $$ = function (s, root) { return [].slice.call((root || document).querySelectorAll(s)); };

  // 语言：点切换时记住；之后打开另一种语言的页面时，按记住的语言跳一次（每个标签页一次）
  var KEY = 'md-lang', pageLang = doc.getAttribute('data-lang');
  var alt = document.querySelector('link[rel=alternate][data-local]');
  try {
    var saved = localStorage.getItem(KEY);
    if (saved && saved !== pageLang && alt && !sessionStorage.getItem('md-lang-jumped')) {
      sessionStorage.setItem('md-lang-jumped', '1');
      location.replace(alt.getAttribute('data-local') + location.search + location.hash);
      return;
    }
  } catch (e) {}
  $$('[data-set-lang]').forEach(function (a) {
    a.addEventListener('click', function () { try { localStorage.setItem(KEY, a.getAttribute('data-set-lang')); } catch (e) {} });
  });

  // 「N 天前」
  $$('[data-ago]').forEach(function (el) {
    var t = Date.parse(el.getAttribute('data-ago'));
    if (!t) return;
    var n = Math.floor((Date.now() - t) / 86400000);
    el.textContent = n <= 0 ? el.getAttribute('data-today') : el.getAttribute('data-tpl').replace('{n}', n);
  });

  // 日历、热力图默认显示最近几周
  $$('.cal-scroll').forEach(function (c) { c.scrollLeft = c.scrollWidth; });

  // 复制（A8：按钮里的像素勾逐格画出）
  window.mdCopy = function (text, btn) {
    function done() { if (btn) { btn.classList.add('is-done'); setTimeout(function () { btn.classList.remove('is-done'); }, 1600); } }
    if (navigator.clipboard && window.isSecureContext !== false) return navigator.clipboard.writeText(text).then(function () { done(); return true; }, function () { return fallback(); });
    return Promise.resolve(fallback());
    function fallback() {
      var ta = document.createElement('textarea'); ta.value = text; ta.setAttribute('readonly', ''); ta.style.position = 'fixed'; ta.style.opacity = '0';
      document.body.appendChild(ta); ta.select();
      var ok = false; try { ok = document.execCommand('copy'); } catch (e) {}
      document.body.removeChild(ta); if (ok) done(); return ok;
    }
  };
  $$('[data-copy]').forEach(function (b) {
    b.addEventListener('click', function () { window.mdCopy(b.getAttribute('data-copy'), b); });
  });

  // 进入视口才播的动画（A4 方格依次放下、A5 数字计数）。减少动效时直接给最终状态。
  function count(el) {
    var target = parseInt(el.getAttribute('data-count'), 10);
    if (!(target > 0) || still) return;
    var t0 = null, dur = 600;
    function step(t) { t0 = t0 || t; var p = Math.min(1, (t - t0) / dur); el.textContent = String(Math.round(target * p)); if (p < 1) requestAnimationFrame(step); }
    el.textContent = '0'; requestAnimationFrame(step);
  }
  var targets = $$('[data-anim]').concat($$('[data-count]'));
  if (still || !('IntersectionObserver' in window)) targets.forEach(function (el) { el.classList.add('is-in'); });
  else {
    var io = new IntersectionObserver(function (es) {
      es.forEach(function (e) {
        if (!e.isIntersecting) return;
        io.unobserve(e.target); e.target.classList.add('is-in');
        if (e.target.hasAttribute('data-count')) count(e.target);
      });
    }, { threshold: 0.2 });
    targets.forEach(function (el) { el.classList.add('will-anim'); io.observe(el); });
  }

  // 点击播放的演示动画（A6）：重新加类以重启动画
  $$('[data-play]').forEach(function (b) {
    var el = document.querySelector(b.getAttribute('data-play'));
    if (!el) return;
    b.addEventListener('click', function () { el.classList.remove('is-playing'); void el.getBoundingClientRect(); el.classList.add('is-playing'); });
  });

  // 版本详情页：手机端选择版本即跳转
  $$('form[data-jump] select').forEach(function (s) {
    s.addEventListener('change', function () { location.href = s.value; });
  });

  // 首页亮点展示台：点左边的名字换右边的面板，不自动翻。窄屏和没有 JS 时所有面板依次排开（见 pages.css）。
  // 面板里的演示在进入视口、被切换到时各播一遍；减少动效时不播，画面本来就是最终帧。
  $$('.showcase').forEach(function (box) {
    var tabs = $$('.sc-tab', box), panels = $$('.sc-panel', box);
    if (!tabs.length || !panels.length) return;
    function play(panel) {
      var d = panel.querySelector('.sc-demo');
      if (!d || still) return;
      d.classList.remove('is-playing'); void d.getBoundingClientRect(); d.classList.add('is-playing');
    }
    function show(id) {
      tabs.forEach(function (t) { if (t.getAttribute('data-sc') === id) t.setAttribute('aria-current', 'true'); else t.removeAttribute('aria-current'); });
      panels.forEach(function (p) { var on = p.id === 'sc-' + id; p.classList.toggle('is-on', on); if (on) play(p); });
    }
    box.classList.add('is-tabs');
    panels[0].classList.add('is-on');
    tabs.forEach(function (t) { t.addEventListener('click', function (e) { e.preventDefault(); show(t.getAttribute('data-sc')); }); });
    if ('IntersectionObserver' in window) {
      var seenIo = new IntersectionObserver(function (es) {
        es.forEach(function (e) { if (e.isIntersecting) { seenIo.unobserve(e.target); play(e.target); } });
      }, { threshold: 0.4 });
      panels.forEach(function (p) { if (p.querySelector('.sc-demo')) seenIo.observe(p); });
    }
  });

  // 页内目录：标出正在看的那一节。取视口上方 40% 处所在的小节，没有 IntersectionObserver 时不标。
  $$('.toc').forEach(function (toc) {
    var links = $$('a[href^="#"]', toc), map = {}, seen = {};
    links.forEach(function (a) { var el = document.getElementById(decodeURIComponent(a.getAttribute('href').slice(1))); if (el) map[el.id] = a; });
    var ids = Object.keys(map);
    if (!ids.length || !('IntersectionObserver' in window)) return;
    function mark() {
      var cur = null;
      ids.forEach(function (id) { if (seen[id]) cur = cur || id; });
      links.forEach(function (a) { if (cur && a === map[cur]) a.setAttribute('aria-current', 'location'); else a.removeAttribute('aria-current'); });
    }
    var spy = new IntersectionObserver(function (es) {
      es.forEach(function (e) { seen[e.target.id] = e.isIntersecting; });
      mark();
    }, { rootMargin: '-10% 0px -55% 0px' });
    ids.forEach(function (id) { spy.observe(document.getElementById(id)); });
  });

  // 菜单：点菜单外关闭
  var menu = document.querySelector('details.menu');
  if (menu) document.addEventListener('click', function (e) { if (menu.open && !menu.contains(e.target)) menu.open = false; });
})();
