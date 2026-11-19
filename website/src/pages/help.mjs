// 帮助（ARCH 4.6）：快速开始、常用入口、常见问题（<details>，每条有锚点）、数据位置与卸载、已知边界。
// content/help.*.json 里的条目允许 <code>，是手写内容，不做转义。
import { esc } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { pageHead, toc, secHead, go } from '../partials/ui.mjs';

export default function help(ctx) {
  const { L, C } = ctx;
  const H = C.help;
  const D = L.docs;
  const moveCard = (msg, cta, href) => `<div class="card more-box"><span>${esc(msg)}</span><div class="help-doc-more">${go(href, cta)}</div></div>`;
  const faqBody = ctx.hasDoc('faq', 'general-faq')
    ? moveCard(D.helpFaqMoved, D.helpFaqCta, ctx.docHref('faq', 'general-faq'))
    : `<div class="faq">${H.faq.items.map((f) => `<details class="card" id="${esc(f.id)}"><summary>${esc(f.q)}</summary><p>${esc(f.a)}</p><a class="anchor" href="#${esc(f.id)}" aria-label="#">#</a></details>`).join('')}</div>`;
  const dataBody = ctx.hasDoc('getting-started', 'data-migration-uninstall')
    ? moveCard(D.helpDataMoved, D.helpDataCta, ctx.docHref('getting-started', 'data-migration-uninstall'))
    : `<ul class="card list">${H.data.items.map((s) => `<li>${s}</li>`).join('')}</ul>`;
  const limitsBody = ctx.hasDoc('getting-started', 'requirements-limits')
    ? moveCard(D.helpLimitsMoved, D.helpLimitsCta, ctx.docHref('getting-started', 'requirements-limits'))
    : `<ul class="card list">${H.limits.items.map((s) => `<li>${esc(s)}</li>`).join('')}</ul>`;
  const sections = [
    { id: 'start', h: H.start.h, body: `<ol class="steps">${H.start.steps.map((s) => `<li>${s}</li>`).join('')}</ol><figure class="card help-shot"><img src="${ctx.a('01-servers.png')}" alt="${esc(L.home.shotAlt)}" width="1440" height="900" loading="lazy"></figure>` },
    { id: 'entries', h: H.entries.h, body: `<ul class="card list">${H.entries.items.map((s) => `<li>${s}</li>`).join('')}</ul>` },
    { id: 'faq', h: H.faq.h, body: faqBody },
    { id: 'data', h: H.data.h, body: dataBody },
    { id: 'limits', h: H.limits.h, body: limitsBody },
  ];
  const main = `${pageHead(ctx, { h: H.title, lead: esc(H.lead), crumbs: [ctx.crumb('home', L.crumbHome), ctx.crumb('help', H.title)] })}
<div class="with-toc">
 <aside class="toc-col">${toc(H.toc, sections.map((s) => ({ id: s.id, text: s.h })))}</aside>
 <div class="toc-main">
 ${sections.map((s) => `<section class="sec-s" id="${s.id}">${secHead({ h: s.h })}${s.body}</section>`).join('\n')}
 <div class="card more-box"><b>${esc(H.more.h)}</b>${go(ctx.href('docs'), D.h)}${go(ctx.href('feedback'), H.more.feedback)}</div>
 </div>
</div>`;
  return layout(ctx, { title: L.help.title, desc: H.desc, main });
}
