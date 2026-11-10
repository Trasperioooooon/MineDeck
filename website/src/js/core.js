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

  // 菜单：点菜单外关闭
  var menu = document.querySelector('details.menu');
  if (menu) document.addEventListener('click', function (e) { if (menu.open && !menu.contains(e.target)) menu.open = false; });
})();
