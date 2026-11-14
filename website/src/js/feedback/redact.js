/* 反馈：脱敏规则（ARCH 6.3）。只作用于生成的报告，不改输入框里的原文。返回 { text, n }。 */
(function (NS) {
  var RULES = [
    // Windows 用户名
    [/([A-Za-z]:\\+Users\\+)([^\\\s"'<>]+)/gi, function (m, a, u) { return u.toLowerCase() === '<user>' ? m : a + '<user>'; }],
    [/(\/home\/|\/Users\/)([^\/\s"'<>]+)/g, function (m, a) { return a + '<user>'; }],
    // 常见密钥形态
    [/\bsk-[A-Za-z0-9_\-]{12,}/g, '<redacted>'],
    // GitHub / Slack / AWS 令牌前缀，CurseForge API Key（$2a$10$ 开头）
    [/\b(?:gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abpr]-[A-Za-z0-9\-]{10,}|AKIA[0-9A-Z]{16})\b/g, '<redacted>'],
    [/\$2[aby]\$\d{2}\$[A-Za-z0-9.\/]{40,}/g, '<redacted>'],
    [/\b(Bearer)\s+[A-Za-z0-9._~+\/\-]+=*/gi, '$1 <redacted>'],
    [/\b((?:api[_-]?)?key|token|password|passwd|secret|access[_-]?key)(\s*[=:]\s*)("?)[^\s"'&,;]+\3/gi, '$1$2<redacted>'],
    [/\b[0-9a-fA-F]{40,}\b/g, '<redacted>'],
    [/\b[A-Za-z0-9+\/]{40,}={0,2}(?![A-Za-z0-9+\/=])/g, '<redacted>'],
    // IPv4：保留 127.0.0.1、0.0.0.0；192.168.x.x 保留前两段
    [/\b(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\b/g, function (m, a, b) {
      if (m === '127.0.0.1' || m === '0.0.0.0') return m;
      if (+a > 255 || +b > 255) return m;
      if (a === '192' && b === '168') return '192.168.x.x';
      return 'x.x.x.x';
    }],
  ];
  NS.redact = function (text) {
    var n = 0, out = String(text || '');
    RULES.forEach(function (r) {
      out = out.replace(r[0], function () {
        var args = arguments, m = args[0];
        var rep = typeof r[1] === 'function' ? r[1].apply(null, args) : r[1].replace(/\$(\d)/g, function (x, i) { return args[+i] || ''; });
        if (rep !== m) n++;
        return rep;
      });
    });
    return { text: out, n: n };
  };
})(window.MDFB = window.MDFB || {});
