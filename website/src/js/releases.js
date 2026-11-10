/* 更新日志列表：渠道筛选，写进 ?channel=（兼容旧的 ?type=）。无 JS 时显示全部。 */
(function () {
  var list = document.getElementById('releases');
  if (!list) return;
  var entries = [].slice.call(list.querySelectorAll('.entry'));
  var groups = [].slice.call(list.querySelectorAll('[data-group]'));
  var chips = [].slice.call(document.querySelectorAll('.chip[data-type]'));
  function current() {
    var m = /[?&](?:channel|type)=(stable|beta)/.exec(location.search);
    return m ? m[1] : 'all';
  }
  function apply(type) {
    entries.forEach(function (e) { e.classList.toggle('is-hidden', !(type === 'all' || e.getAttribute('data-type') === type)); });
    groups.forEach(function (g) { g.classList.toggle('is-hidden', !g.querySelector('.entry:not(.is-hidden)')); });
    chips.forEach(function (c) { c.setAttribute('aria-pressed', String(c.getAttribute('data-type') === type)); });
  }
  chips.forEach(function (c) {
    c.addEventListener('click', function () {
      var t = c.getAttribute('data-type');
      try { history.replaceState(null, '', location.pathname + (t === 'all' ? '' : '?channel=' + t) + location.hash); } catch (e) {}
      apply(t);
    });
  });
  apply(current());
})();
