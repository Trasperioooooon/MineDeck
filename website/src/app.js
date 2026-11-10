/* MineDeck 官网脚本。全部是增强：关掉 JS 时页面内容、下载链接和所有版本仍然可用。 */
(function () {
  var doc = document.documentElement;
  doc.classList.add('js');
  var KEY = 'md-lang';
  var pageLang = doc.getAttribute('data-lang');
  var alt = document.querySelector('link[rel=alternate][data-local]');

  // 语言：点切换时记住选择；之后打开另一种语言的页面时，按记住的语言跳一次
  try {
    var saved = localStorage.getItem(KEY);
    if (saved && saved !== pageLang && alt && !sessionStorage.getItem('md-lang-jumped')) {
      sessionStorage.setItem('md-lang-jumped', '1');
      location.replace(alt.getAttribute('data-local') + location.search + location.hash);
      return;
    }
  } catch (e) {}
  document.querySelectorAll('[data-set-lang]').forEach(function (a) {
    a.addEventListener('click', function () { try { localStorage.setItem(KEY, a.getAttribute('data-set-lang')); } catch (e) {} });
  });

  // 「N 天前」
  document.querySelectorAll('[data-ago]').forEach(function (el) {
    var t = Date.parse(el.getAttribute('data-ago'));
    if (!t) return;
    var n = Math.floor((Date.now() - t) / 86400000);
    el.textContent = n <= 0 ? el.getAttribute('data-today') : el.getAttribute('data-tpl').replace('{n}', n);
  });

  // 发布日历默认显示最近几周
  document.querySelectorAll('.cal-scroll').forEach(function (c) { c.scrollLeft = c.scrollWidth; });

  // 发布说明折叠
  document.querySelectorAll('.notes').forEach(function (n) {
    var btn = n.parentNode.querySelector('.toggle');
    if (!btn) return;
    if (n.scrollHeight < 150) { btn.remove(); return; }
    n.classList.add('clamp');
    btn.addEventListener('click', function () {
      var open = n.classList.toggle('clamp');
      btn.textContent = open ? btn.getAttribute('data-more') : btn.getAttribute('data-less');
      btn.setAttribute('aria-expanded', String(!open));
    });
  });

  // 筛选与「查看全部」
  var list = document.getElementById('releases');
  if (!list) return;
  var entries = [].slice.call(list.querySelectorAll('.entry'));
  var groups = [].slice.call(list.querySelectorAll('[data-group]'));
  var initial = parseInt(list.getAttribute('data-initial'), 10) || 10;
  var moreBtn = document.getElementById('show-all');
  var expanded = false;
  var chips = [].slice.call(document.querySelectorAll('.chip'));
  function current() {
    var m = /[?&]type=(stable|beta)/.exec(location.search);
    return m ? m[1] : 'all';
  }
  function apply(type) {
    var shown = 0;
    // 通过 #锚点 打开某个版本时直接全部展开
    if (location.hash && document.getElementById(location.hash.slice(1))) expanded = true;
    entries.forEach(function (e) {
      var ok = type === 'all' || e.getAttribute('data-type') === type;
      var vis = ok && (expanded || shown < initial);
      if (ok && vis) shown++;
      e.classList.toggle('is-hidden', !vis);
    });
    groups.forEach(function (g) {
      g.classList.toggle('is-hidden', !g.querySelector('.entry:not(.is-hidden)'));
    });
    var total = entries.filter(function (e) { return type === 'all' || e.getAttribute('data-type') === type; }).length;
    if (moreBtn) {
      moreBtn.classList.toggle('is-hidden', expanded || total <= initial);
      moreBtn.textContent = moreBtn.getAttribute('data-tpl').replace('{n}', total);
    }
    chips.forEach(function (c) { c.setAttribute('aria-pressed', String(c.getAttribute('data-type') === type)); });
  }
  chips.forEach(function (c) {
    c.addEventListener('click', function () {
      var t = c.getAttribute('data-type');
      var url = location.pathname + (t === 'all' ? '' : '?type=' + t);
      try { history.replaceState(null, '', url); } catch (e) {}
      apply(t);
    });
  });
  if (moreBtn) moreBtn.addEventListener('click', function () { expanded = true; apply(current()); });
  window.addEventListener('hashchange', function () { apply(current()); });
  apply(current());
})();
