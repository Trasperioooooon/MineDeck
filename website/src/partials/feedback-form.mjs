// 反馈表单（ARCH 6）。字段与 .github/ISSUE_TEMPLATE/beta-feedback.md 对应；提交通道由 site.config.json 的 feedback.channels 决定。
// 浏览器端逻辑在 src/js/feedback/*：redact（脱敏）→ report（生成报告与 minedeck.feedback/v1 载荷）→ channels（提交通道）→ form（交互）。
import { esc } from './util.mjs';
import { icon } from './svg.mjs';

const field = (id, label, control, { hint, req, cls = '', show } = {}) =>
  `<div class="field ${cls}"${show ? ` data-show="${esc(show)}"` : ''}><label for="${id}">${esc(label)}${req ? ' <span class="req" aria-hidden="true">*</span>' : ''}</label>${control}${hint ? `<span class="hint" id="${id}-hint">${esc(hint)}</span>` : ''}</div>`;

/** 只把启用的通道交给前端；endpoint 未启用时不输出地址 */
export function publicChannels(cfg) {
  return (cfg.feedback.channels || []).filter((c) => {
    if (c.type === 'endpoint') return c.enabled && c.url;
    if (c.type === 'email') return c.enabled && c.address;
    if (c.type === 'community') return c.enabled && (c.value || c.url);
    return c.enabled !== false;
  });
}

export function feedbackForm(ctx) {
  const { L, C, data, cfg } = ctx;
  const F = L.feedback;
  const st = data.latest_stable;
  const versions = data.releases.map((r) => `<option value="${esc(r.tag)}" data-channel="${r.channel}"${st && r.tag === st.tag ? ' selected' : ''}>${esc(r.tag)}${r.prerelease ? ' · ' + esc(L.log.betaF) : ''}</option>`).join('') + `<option value="">${esc(F.unsure)}</option>`;
  const radios = (name, opts, checked) => `<div class="radios" role="radiogroup">${Object.entries(opts).map(([v, t]) => `<label class="chip-r"><input type="radio" name="${name}" value="${v}"${v === checked ? ' checked' : ''}><span>${esc(t)}</span></label>`).join('')}</div>`;
  const cfgJson = {
    repo: cfg.repo, siteBuild: data.generated_at, lang: ctx.lang, channels: publicChannels(cfg),
    msg: { redacted: F.redacted, required: F.required, githubLong: F.githubLong, sending: F.sending, sent: F.sent, sendFail: F.sendFail, copied: F.copiedMsg, copyManual: F.copyManual },
    labels: { kinds: F.kinds, results: F.results, areas: F.areaNames, title: F.titleF, version: F.version, os: F.os, steps: F.steps, expected: F.expected, actual: F.actual, confirm: F.confirm, caseId: F.caseId, result: F.result },
  };
  const btn = (c) => ({
    copy: `<button type="button" class="btn primary" data-channel="copy">${icon('copy', { cls: 'btn-ico i-copy' })}${icon('check', { cls: 'btn-ico i-check' })}<span>${esc(F.copyBtn)}</span></button>`,
    download: `<button type="button" class="btn" data-channel="download"><span>${esc(F.downloadBtn)}</span></button>`,
    github: `<span class="ch-gh"><button type="button" class="btn ext" data-channel="github"><span>${esc(F.githubBtn)}</span></button><small>${esc(F.githubNote)} · ${esc(L.fallback)}</small></span>`,
    endpoint: `<button type="button" class="btn primary" data-channel="endpoint"><span>${esc(c.label || F.endpointBtn)}</span></button>`,
    email: `<button type="button" class="btn" data-channel="email"><span>Email</span></button>`,
    community: `<span class="ch-com"><b>${esc(c.label)}</b> ${esc(c.value || '')}</span>`,
  })[c.type] || '';
  return `<noscript><p class="notice">${esc(F.noJs)} <a href="https://github.com/${esc(cfg.repo)}/issues/new" data-ext target="_blank" rel="noopener">GitHub Issues ↗</a></p></noscript>
<form class="form card js-only" id="fb" novalidate autocomplete="off">
 <script type="application/json" id="fb-config">${JSON.stringify(cfgJson).replace(/</g, '\\u003c')}</script>
 <p class="notice">${esc(F.warn)}</p>
 <fieldset class="field"><legend>${esc(F.kind)} <span class="req" aria-hidden="true">*</span></legend>${radios('kind', F.kinds, 'bug')}</fieldset>
 <div class="row2" data-show="beta">
  ${field('fb-case', F.caseId, '<input id="fb-case" name="caseId" type="text" maxlength="40">', { hint: F.caseIdHint })}
  <fieldset class="field"><legend>${esc(F.result)}</legend>${radios('result', F.results, 'fail')}</fieldset>
 </div>
 ${field('fb-title', F.titleF, '<input id="fb-title" name="title" type="text" maxlength="80" required aria-describedby="fb-title-hint">', { hint: F.titleHint, req: true })}
 <div class="row2">
  ${field('fb-version', F.version, `<select id="fb-version" name="version">${versions}</select>`, { hint: F.versionHint, req: true })}
  ${field('fb-os', F.os, `<select id="fb-os" name="os"><option>Windows 11</option><option>Windows 10</option><option value="other">${esc(F.osOther)}</option></select>`, { req: true })}
 </div>
 <div class="row2">
  ${field('fb-osbuild', F.osBuild, '<input id="fb-osbuild" name="osBuild" type="text" maxlength="40" placeholder="26100.2314">', { hint: F.osBuildHint })}
  ${field('fb-mem', F.mem, '<input id="fb-mem" name="memoryGB" type="number" min="1" max="1024" inputmode="numeric">')}
 </div>
 <div class="row3">
  ${field('fb-core', F.core, `<select id="fb-core" name="core"><option value="">${esc(F.coreNone)}</option>${C.features.cores.map((c) => `<option>${esc(c)}</option>`).join('')}</select>`)}
  ${field('fb-mc', F.mc, '<input id="fb-mc" name="mcVersion" type="text" maxlength="20" placeholder="1.21.1">')}
  ${field('fb-corever', F.coreVer, '<input id="fb-corever" name="coreVersion" type="text" maxlength="40" placeholder="21.1.77">')}
 </div>
 ${field('fb-modpack', F.modpack, '<input id="fb-modpack" name="modpack" type="text" maxlength="120">')}
 <fieldset class="field"><legend>${esc(F.areas)}</legend><div class="checks">${Object.entries(F.areaNames).map(([v, t]) => `<label class="chip-r"><input type="checkbox" name="areas" value="${v}"><span>${esc(t)}</span></label>`).join('')}</div></fieldset>
 <div data-show-area="ai">${field('fb-ai', F.aiModel, '<input id="fb-ai" name="ai" type="text" maxlength="80" placeholder="DeepSeek · deepseek-chat">', { hint: F.aiHint })}</div>
 ${field('fb-steps', F.steps, '<textarea id="fb-steps" name="steps" rows="4">1. </textarea>', { cls: 'need-bug', req: true })}
 <div class="row2">
  ${field('fb-expected', F.expected, '<textarea id="fb-expected" name="expected" rows="3"></textarea>', { cls: 'need-bug', req: true })}
  ${field('fb-actual', F.actual, '<textarea id="fb-actual" name="actual" rows="3"></textarea>', { cls: 'need-bug', req: true })}
 </div>
 ${field('fb-logs', F.logs, '<textarea id="fb-logs" name="logs" rows="6" class="mono" maxlength="60000" spellcheck="false"></textarea>', { hint: F.logsHint, cls: 'drop' })}
 <p class="hint">${esc(F.shots)}</p>
 ${field('fb-contact', F.contact, '<input id="fb-contact" name="contact" type="text" maxlength="80">', { hint: F.contactHint, show: 'endpoint' })}
 <input type="text" name="website" class="hp" tabindex="-1" autocomplete="off" aria-hidden="true">
 <label class="confirm"><input type="checkbox" name="confirm" required> ${esc(F.confirm)}</label>
 <div class="field"><label for="fb-preview">${esc(F.preview)}</label><textarea id="fb-preview" class="mono" rows="10" readonly></textarea><span class="hint" id="fb-redact" aria-live="polite"></span></div>
 <div class="fb-submit"><b class="sub-h">${esc(F.submitH)}</b><div class="cta">${publicChannels(cfg).map(btn).join('')}</div><p class="fb-status" role="status" aria-live="polite"></p></div>
 <p class="hint">${esc(F.draft)} <button type="button" class="linkish" data-clear>${esc(F.clear)}</button></p>
</form>`;
}
