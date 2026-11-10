// 关于（ARCH 4.8）：简介、贡献者（本地头像）、活动（52 周热力图 + 每月发布数）、参与贡献、许可证全文、致谢、声明。
import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { pageHead, secHead, extLink, button, toc } from '../partials/ui.mjs';
import { heatmap, monthlyBars } from '../partials/heatmap.mjs';
import { avatarRow } from '../partials/people.mjs';
import { repo } from '../routes.mjs';

export default function about(ctx) {
  const { L, data, cfg } = ctx;
  const A = L.about;
  const asOf = (iso) => (iso ? fmt(A.contribAsOf, { date: ctx.fmtDate(iso) }) : '');
  const join = A.join.map((j) => j.ext
    ? `<div class="card join">${extLink(ctx, repo(cfg) + '/pulls', j.t)}<p>${esc(j.d)}</p></div>`
    : `<a class="card join" href="${ctx.href(j.to)}${j.q || ''}"><b>${esc(j.t)} →</b><p>${esc(j.d)}</p></a>`).join('');
  const sections = [
    { id: 'intro', h: A.introH, body: A.intro.map((p) => `<p>${esc(fmt(p, { renamed: data.renamed_in || '' }))}</p>`).join('') },
    { id: 'contributors', h: A.contribH, p: esc(asOf(data.contributors.fetched_at)), body: avatarRow(ctx, { max: 100, showCount: true }) + (data.contributors.hidden && cfg.contributors.showHiddenNote ? `<p class="note">${esc(A.hiddenNote)}</p>` : '') },
    { id: 'activity', h: A.activityH, p: esc(A.activitySub), body: heatmap(ctx, { weeks: 52 }) + `<h3 class="sub-h">${esc(A.monthly)}</h3>` + monthlyBars(ctx) },
    { id: 'contribute', h: A.joinH, body: `<div class="grid cols-3">${join}</div>` + (cfg.stats.showStars && data.repoInfo ? `<p class="star-line">${esc(fmt(A.starP, { n: data.repoInfo.stargazers_count, date: ctx.fmtDate(data.repoInfo.fetched_at) }))} ${button({ href: repo(cfg), label: L.home.star, size: 'sm', ext: true, ctx })}</p>` : '') },
    { id: 'license', h: A.licenseH, body: `<p>${esc(A.licenseP)}</p><pre class="card license">${esc(data.license)}</pre>` },
    { id: 'thanks', h: A.thanksH, body: `<ul class="card list">${A.thanks.map((t) => `<li>${esc(t)}</li>`).join('')}<li><a href="${ctx.link('/assets/licenses/')}">${esc(L.footer.fontLicenses)}</a></li></ul>` },
    { id: 'disclaimer', h: A.disclaimerH, body: `<p class="card list">${esc(L.footer.disclaimer)}</p>` },
  ];
  const main = `${pageHead(ctx, { h: A.h, crumbs: [ctx.crumb('home', L.crumbHome), ctx.crumb('about', L.nav.about)] })}
<div class="with-toc">
 <aside class="toc-col">${toc(ctx.C.help.toc, sections.map((s) => ({ id: s.id, text: s.h })))}</aside>
 <div class="toc-main">${sections.map((s) => `<section class="sec-s" id="${s.id}">${secHead({ h: s.h, p: s.p })}${s.body}</section>`).join('\n')}</div>
</div>`;
  return layout(ctx, { title: A.title, desc: A.desc, main });
}
