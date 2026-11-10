// 反馈（ARCH 6）：结构化表单，默认通道为复制报告；GitHub 预填链接为备用；HTTP 端点默认关闭。
import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { pageHead } from '../partials/ui.mjs';
import { feedbackForm } from '../partials/feedback-form.mjs';

export default function feedback(ctx) {
  const { L, data } = ctx;
  const F = L.feedback;
  const latest = data.releases[0];
  const before = `<div class="card before"><b>${esc(F.before)}</b><a href="${ctx.href('help')}#faq">${esc(F.beforeFaq)} →</a><a href="${ctx.href('help')}#limits">${esc(F.beforeLimits)} →</a>${latest ? `<a href="${ctx.href('release', { tag: latest.tag })}">${esc(fmt(F.beforeLatest, { tag: latest.tag }))} →</a>` : ''}</div>`;
  const main = `${pageHead(ctx, { h: F.h, lead: esc(F.lead), crumbs: [ctx.crumb('home', L.crumbHome), ctx.crumb('feedback', L.nav.feedback)] })}
${before}
<div class="fb-layout">
 <div>${feedbackForm(ctx)}</div>
 <aside class="fb-side">
  <div class="card"><b>${esc(F.logsH)}</b><ul>${F.logsItems.map((s) => `<li>${esc(s)}</li>`).join('')}</ul></div>
  <div class="card"><b>${esc(F.verH)}</b><p>${esc(F.verP)}</p></div>
 </aside>
</div>`;
  return layout(ctx, { title: F.title, desc: F.desc, main });
}
