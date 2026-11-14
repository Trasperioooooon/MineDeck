/* 反馈：从表单生成报告 Markdown（与 .github/ISSUE_TEMPLATE/beta-feedback.md 的格式兼容）和 minedeck.feedback/v1 载荷。 */
(function (NS) {
  var HEAD = 200, TAIL = 400, MAX = 60000;
  NS.truncateLog = function (s) {
    s = String(s || '');
    var lines = s.split(/\r?\n/);
    if (s.length <= MAX && lines.length <= HEAD + TAIL) return { text: s, truncated: false };
    var cut = lines.length - HEAD - TAIL;
    var text = lines.slice(0, HEAD).join('\n') + '\n… 已省略 ' + Math.max(cut, 0) + ' 行 …\n' + lines.slice(-TAIL).join('\n');
    if (text.length > MAX) text = text.slice(0, MAX / 2) + '\n…\n' + text.slice(-MAX / 2);
    return { text: text, truncated: true };
  };

  /** 读表单 → 原始字段 */
  NS.collect = function (form) {
    var v = function (n) { var el = form.elements[n]; return el ? String(el.value || '').trim() : ''; };
    var areas = [].slice.call(form.querySelectorAll('input[name=areas]:checked')).map(function (x) { return x.value; });
    var kind = (form.querySelector('input[name=kind]:checked') || {}).value || 'bug';
    var verSel = form.elements.version;
    var ch = verSel && verSel.selectedOptions && verSel.selectedOptions[0] ? verSel.selectedOptions[0].getAttribute('data-channel') : null;
    return {
      kind: kind, title: v('title'), version: v('version'), channel: ch,
      os: v('os'), osBuild: v('osBuild'), memoryGB: v('memoryGB') ? +v('memoryGB') : null,
      core: v('core'), mcVersion: v('mcVersion'), coreVersion: v('coreVersion'), modpack: v('modpack'),
      areas: areas, ai: areas.indexOf('ai') >= 0 ? v('ai') : '', caseId: v('caseId'),
      result: (form.querySelector('input[name=result]:checked') || {}).value || 'fail',
      steps: v('steps'), expected: v('expected'), actual: v('actual'), logs: v('logs'), contact: v('contact'),
      honeypot: v('website'), confirm: !!(form.elements.confirm && form.elements.confirm.checked),
    };
  };

  /** 原始字段 → { markdown, payload, redactions } */
  NS.build = function (f, cfg, meta) {
    var L = cfg.labels, R = NS.redact;
    var red = 0;
    var clean = function (s) { var r = R(s); red += r.n; return r.text; };
    var log = NS.truncateLog(f.logs);
    var logs = clean(log.text);
    var steps = clean(f.steps), expected = clean(f.expected), actual = clean(f.actual), title = clean(f.title);
    var pc = [f.os === 'other' ? '' : f.os, f.osBuild, f.memoryGB ? f.memoryGB + ' GB' : ''].filter(Boolean).join(' · ');
    var server = [f.core, f.mcVersion, f.coreVersion].filter(Boolean).join(' ') + (f.modpack ? '；整合包 ' + f.modpack : '');
    var md = [];
    if (f.kind === 'beta') { md.push('【编号】' + (f.caseId || '')); md.push('【结果】' + (L.results[f.result] || f.result)); }
    else md.push('【类型】' + (L.kinds[f.kind] || f.kind));
    md.push('【标题】' + title);
    md.push('【版本】' + (f.version || '不确定'));
    md.push('【电脑】' + pc);
    md.push('【服务器】' + server);
    md.push('【涉及】' + f.areas.map(function (a) { return L.areas[a] || a; }).join('、'));
    md.push('【AI】' + clean(f.ai));
    md.push('【步骤】\n' + steps);
    md.push('【预期】\n' + expected);
    md.push('【实际】\n' + actual);
    md.push('【附件】' + (logs ? '\n```\n' + logs + '\n```' : ''));
    md.push('\n<!-- 来自 minedeck.cn 反馈页 · ' + meta.siteBuild + ' -->');
    var markdown = md.join('\n');
    var stepList = steps.split(/\n/).map(function (s) { return s.replace(/^\s*\d+[.、)]\s*/, '').trim(); }).filter(Boolean);
    var contact = /@/.test(f.contact) ? { email: f.contact, qq: '' } : { email: '', qq: f.contact };
    var payload = {
      schema: 'minedeck.feedback/v1', kind: f.kind, title: title,
      app: { version: f.version, channel: f.channel }, env: { os: f.os, osBuild: f.osBuild, memoryGB: f.memoryGB },
      server: { core: f.core, mcVersion: f.mcVersion, coreVersion: f.coreVersion, modpack: f.modpack },
      areas: f.areas, ai: f.ai ? { model: clean(f.ai) } : null,
      steps: stepList, expected: expected, actual: actual,
      logs: { text: logs, redactions: red, truncated: log.truncated, chars: f.logs.length },
      contact: contact, reportMarkdown: markdown,
      meta: { lang: cfg.lang, page: location.pathname, referrer: document.referrer ? new URL(document.referrer).pathname : '', siteBuild: cfg.siteBuild, submittedAt: new Date().toISOString(), userAgent: navigator.userAgent, honeypot: f.honeypot, fillMs: Date.now() - meta.openedAt },
    };
    if (f.kind === 'beta') payload.beta = { caseId: f.caseId, result: f.result };
    return { markdown: markdown, payload: payload, redactions: red, logs: logs };
  };
})(window.MDFB = window.MDFB || {});
