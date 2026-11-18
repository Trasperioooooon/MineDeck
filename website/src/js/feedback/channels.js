/* 反馈：可插拔提交通道（ARCH 6.4）。每个通道 { type, available(cfg), submit(report, cfg, ctx) → Promise<{ok, message, url?}> } */
(function (NS) {
  var fmt = function (s, v) { return String(s).replace(/\{(\w+)\}/g, function (m, k) { return k in v ? v[k] : m; }); };
  var C = {};
  C.copy = {
    available: function () { return true; },
    submit: function (r, cfg, ctx) {
      return Promise.resolve(window.mdCopy(r.markdown, ctx.button)).then(function (ok) {
        if (ok) return { ok: true, message: cfg.msg.copied };
        ctx.preview.focus(); ctx.preview.select();
        return { ok: false, message: cfg.msg.copyManual };
      });
    },
  };
  C.download = {
    available: function () { return typeof Blob !== 'undefined'; },
    submit: function (r) {
      var a = document.createElement('a');
      a.href = URL.createObjectURL(new Blob([r.markdown], { type: 'text/markdown;charset=utf-8' }));
      a.download = 'MineDeck-反馈-' + new Date().toISOString().slice(0, 10) + '.md';
      document.body.appendChild(a); a.click(); a.remove();
      setTimeout(function () { URL.revokeObjectURL(a.href); }, 5000);
      return Promise.resolve({ ok: true, message: '' });
    },
  };
  C.github = {
    available: function () { return true; },
    submit: function (r, cfg, ctx) {
      var ch = ctx.channel, base = 'https://github.com/' + cfg.repo + '/issues/new';
      var q = function (body) { return base + '?title=' + encodeURIComponent((r.payload.kind === 'beta' ? '[测试反馈] ' : '') + r.payload.title) + '&body=' + encodeURIComponent(body) + (ch.labels && ch.labels.length ? '&labels=' + encodeURIComponent(ch.labels.join(',')) : ''); };
      var url = q(r.markdown), msg = '';
      if (url.length > (ch.maxUrl || 7000)) {
        url = q(r.markdown.replace(/【附件】[\s\S]*?(?=\n<!--)/, '【附件】（日志见下方粘贴）'));
        window.mdCopy(r.logs || r.markdown);
        msg = cfg.msg.githubLong;
      }
      window.open(url, '_blank', 'noopener');
      return Promise.resolve({ ok: true, message: msg, url: url });
    },
  };
  C.endpoint = {
    available: function (ch) { return !!(ch.enabled && ch.url && window.fetch); },
    submit: function (r, cfg, ctx) {
      var ch = ctx.channel, ctrl = window.AbortController ? new AbortController() : null;
      var timer = setTimeout(function () { if (ctrl) ctrl.abort(); }, ch.timeoutMs || 15000);
      var body = JSON.stringify(r.payload);
      if (body.length > 128 * 1024) return Promise.resolve({ ok: false, message: fmt(cfg.msg.sendFail, { msg: 'too_large' }) });
      return fetch(ch.url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: body, signal: ctrl && ctrl.signal })
        .then(function (res) { return res.json().catch(function () { return {}; }).then(function (j) { return { res: res, j: j }; }); })
        .then(function (x) {
          clearTimeout(timer);
          if (x.res.ok && x.j.ok) return { ok: true, message: x.j.message || fmt(cfg.msg.sent, { id: x.j.id || '' }), url: x.j.issueUrl || null, sent: true };
          return { ok: false, message: fmt(cfg.msg.sendFail, { msg: x.j.message || x.j.error || ('HTTP ' + x.res.status) }) };
        }, function (e) { clearTimeout(timer); return { ok: false, message: fmt(cfg.msg.sendFail, { msg: e.name === 'AbortError' ? 'timeout' : e.message }) }; });
    },
  };
  C.email = {
    available: function (ch) { return !!(ch.enabled && ch.address); },
    submit: function (r, cfg, ctx) {
      var body = r.markdown.length > 1500 ? r.markdown.slice(0, 1500) + '\n…（完整报告已复制，请粘贴到这里）' : r.markdown;
      if (r.markdown.length > 1500) window.mdCopy(r.markdown);
      location.href = 'mailto:' + ctx.channel.address + '?subject=' + encodeURIComponent('[MineDeck] ' + r.payload.title) + '&body=' + encodeURIComponent(body);
      return Promise.resolve({ ok: true, message: '' });
    },
  };
  NS.channels = C;
})(window.MDFB = window.MDFB || {});
