// 文档中心首页（/docs/ 与 /en/docs/）：大搜索框、新手必读（front-matter featured 驱动）、分类卡片网格与版本说明入口。
import { esc } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { pageHead, secHead, go } from '../partials/ui.mjs';
import { icon } from '../partials/svg.mjs';
import { docSearch } from '../partials/doc-search.mjs';

export default function docsHome(ctx) {
  const { L, data, lang } = ctx;
  const D = L.docs;
  const docsModel = data.docs;
  const tree = docsModel.treeByLang[lang];
  const flat = docsModel.flatByLang[lang];

  const featured = flat.filter((d) => d.featured && !d.untranslated);
  const featuredHtml = featured.length ? `<section class="sec-s doc-home-sec">
 ${secHead({ h: D.featuredH, p: esc(D.featuredP) })}
 <div class="grid cols-3 doc-featured">
  ${featured.map((d) => {
    const badges = [
      `<span class="eyebrow">${esc(d.catTitle)}</span>`,
      d.since ? `<span class="badge st">${esc(D.sincePrefix)} ${esc(d.since)}</span>` : '',
      d.beta ? `<span class="badge bt">${esc(L.log.betaF)}</span>` : '',
    ].filter(Boolean).join('');
    return `<a class="card feat doc-feat-card" href="${esc(ctx.link(d.url))}">
   <div class="doc-feat-meta">${badges}</div>
   <h3>${esc(d.title)}</h3>
   <p>${esc(d.description)}</p>
  </a>`;
  }).join('\n')}
 </div>
</section>` : '';

  const catsHtml = `<section class="sec-s doc-home-sec">
 ${secHead({ h: D.categoriesH, p: esc(D.categoriesP) })}
 <div class="grid cols-2 doc-cats">
  ${tree.map((c, ci) => {
    const items = c.items.map((it) => {
      const beta = it.beta ? ` <span class="badge bt">${esc(L.log.betaF)}</span>` : '';
      const untr = it.untranslated ? ` <span class="doc-untr-tag">${esc(D.untranslatedBadge)}</span>` : '';
      return `<li><a href="${esc(ctx.link(it.url))}"><span>${esc(it.title)}</span>${beta}${untr}</a><small>${esc(it.description)}</small></li>`;
    }).join('');
    return `<section class="card doc-cat-card" id="cat-${esc(c.id)}">
   <div class="doc-cat-head">
    <span class="ico" style="--c:var(--icon-${ci % 12})">${icon(c.icon || 'block', { cls: 'feat-ico' })}</span>
    <div>
     <h3 class="pix">${esc(c.title)}</h3>
     <p class="muted">${esc(c.desc)}</p>
    </div>
   </div>
   <ul class="doc-cat-list">${items}</ul>
  </section>`;
  }).join('\n')}
 </div>
</section>`;

  const extraStrip = `<div class="card oss doc-home-rel">
 <div>
  <h2 class="pix">${esc(D.relBandH)}</h2>
  <p>${esc(D.relBandP)}</p>
 </div>
 <div class="row">
  ${go(ctx.href('releases'), D.relBandLink)}
  ${go(ctx.href('help'), D.helpQuickLink)}
 </div>
</div>`;

  const main = `${pageHead(ctx, { h: D.h, lead: esc(D.lead), crumbs: [ctx.crumb('home', L.crumbHome), ctx.crumb('docs', D.h)] })}
<section class="sec page-top doc-home">
 ${docSearch(ctx, { variant: 'hero', idPrefix: 'doc-home-s' })}
 ${featuredHtml}
 ${catsHtml}
 ${extraStrip}
</section>`;

  return layout(ctx, { title: D.title, desc: D.desc, main });
}
