/* 反馈：表单交互。显示/隐藏条件字段、草稿（localStorage md.feedback.draft）、日志拖入、实时预览、校验、调用通道。 */
(function (NS) {
  var form = document.getElementById('fb');
  if (!form) return;
  var cfg = JSON.parse(document.getElementById('fb-config').textContent);
  var preview = document.getElementById('fb-preview');
  var redactNote = document.getElementById('fb-redact');
  var status = form.querySelector('.fb-status');
  var DRAFT = 'md.feedback.draft';
  var openedAt = Date.now();
  var fmt = function (s, v) { return String(s).replace(/\{(\w+)\}/g, function (m, k) { return k in v ? v[k] : m; }); };
  var hasEndpoint = cfg.channels.some(function (c) { return c.type === 'endpoint'; });

  function sync() {
    var f = NS.collect(form);
    form.querySelectorAll('[data-show]').forEach(function (el) {
      var k = el.getAttribute('data-show');
      el.hidden = k === 'endpoint' ? !hasEndpoint : f.kind !== k;
    });
    form.querySelectorAll('[data-show-area]').forEach(function (el) { el.hidden = f.areas.indexOf(el.getAttribute('data-show-area')) < 0; });
    form.querySelectorAll('.need-bug .req').forEach(function (el) { el.hidden = f.kind !== 'bug'; });
    var r = NS.build(f, cfg, { siteBuild: cfg.siteBuild, openedAt: openedAt });
    preview.value = r.markdown;
    redactNote.textContent = r.redactions ? fmt(cfg.msg.redacted, { n: r.redactions }) : '';
    return { f: f, r: r };
  }
  function save() {
    var o = {};
    [].forEach.call(form.elements, function (el) {
      if (!el.name || el.name === 'website' || el.type === 'button') return;
      if (el.type === 'checkbox') { if (el.name === 'areas') { (o.areas = o.areas || []); if (el.checked) o.areas.push(el.value); } else o[el.name] = el.checked; }
      else if (el.type === 'radio') { if (el.checked) o[el.name] = el.value; }
      else o[el.name] = el.value;
    });
    try { localStorage.setItem(DRAFT, JSON.stringify(o)); } catch (e) {}
  }
  function load() {
    var o; try { o = JSON.parse(localStorage.getItem(DRAFT) || 'null'); } catch (e) {}
    if (o) [].forEach.call(form.elements, function (el) {
      if (!el.name || !(el.name in o)) return;
      if (el.type === 'checkbox') el.checked = el.name === 'areas' ? o.areas.indexOf(el.value) >= 0 : !!o[el.name];
      else if (el.type === 'radio') el.checked = o[el.name] === el.value;
      else el.value = o[el.name];
    });
    // URL 参数优先：?version=v3.1.0-beta.3、?kind=beta
    var p = new URLSearchParams(location.search);
    if (p.get('version') && form.elements.version.querySelector('option[value="' + CSS.escape(p.get('version')) + '"]')) form.elements.version.value = p.get('version');
    var k = p.get('kind'); if (k) { var rb = form.querySelector('input[name=kind][value="' + CSS.escape(k) + '"]'); if (rb) rb.checked = true; }
  }
  function missing(f) {
    var L = cfg.labels, m = [];
    if (!f.title) m.push(L.title);
    if (f.kind === 'bug') { if (!f.steps.replace(/^1\.\s*$/, '')) m.push(L.steps); if (!f.expected) m.push(L.expected); if (!f.actual) m.push(L.actual); }
    if (f.kind === 'beta' && !f.caseId) m.push(L.caseId);
    if (!f.confirm) m.push(L.confirm);
    return m;
  }
  function say(msg, ok) { status.textContent = msg || ''; status.classList.toggle('ok', !!ok); }

  load(); sync();
  form.addEventListener('input', function () { sync(); save(); });
  form.addEventListener('change', function () { sync(); save(); });
  form.addEventListener('submit', function (e) { e.preventDefault(); });

  // 日志文件拖入：只在浏览器本地读取
  var logs = form.elements.logs;
  logs.addEventListener('dragover', function (e) { e.preventDefault(); logs.classList.add('is-drag'); });
  logs.addEventListener('dragleave', function () { logs.classList.remove('is-drag'); });
  logs.addEventListener('drop', function (e) {
    e.preventDefault(); logs.classList.remove('is-drag');
    var file = e.dataTransfer.files && e.dataTransfer.files[0];
    if (!file || !/\.(log|txt)$/i.test(file.name)) return;
    file.text().then(function (t) { logs.value = NS.truncateLog(t).text; sync(); save(); });
  });

  form.querySelectorAll('[data-channel]').forEach(function (btn) {
    btn.addEventListener('click', function () {
      var s = sync();
      var m = missing(s.f);
      if (m.length) { say(fmt(cfg.msg.required, { fields: m.join('、') })); return; }
      var type = btn.getAttribute('data-channel');
      var ch = cfg.channels.filter(function (c) { return c.type === type; })[0] || { type: type };
      var impl = NS.channels[type];
      if (!impl || !impl.available(ch)) return;
      if (type === 'endpoint') say(cfg.msg.sending);
      btn.disabled = true;
      impl.submit(s.r, cfg, { channel: ch, button: btn, preview: preview }).then(function (res) {
        btn.disabled = false;
        say(res.message, res.ok);
        if (res.sent) { try { localStorage.removeItem(DRAFT); } catch (e) {} }
      });
    });
  });
  form.querySelector('[data-clear]').addEventListener('click', function () {
    try { localStorage.removeItem(DRAFT); } catch (e) {}
    form.reset(); sync(); say('');
  });
})(window.MDFB = window.MDFB || {});
