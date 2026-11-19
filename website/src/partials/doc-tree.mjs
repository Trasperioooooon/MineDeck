// 文档左侧目录树：按 _meta.json 分类折叠，当前分类默认展开、当前文章高亮，兼容无 JS <details>。
import { esc } from './util.mjs';
import { icon } from './svg.mjs';

export function docTree(ctx, tree, { currentCat, currentSlug, searchHtml = '' } = {}) {
  const { L } = ctx;
  const D = L.docs;
  const cats = tree.map((c) => {
    const isCurrentCat = c.id === currentCat;
    const links = c.items.map((it) => {
      const cur = isCurrentCat && it.slug === currentSlug;
      const beta = it.beta ? ` <span class="badge bt">${esc(L.log.betaF)}</span>` : '';
      const raw = it.untranslated ? ` <span class="doc-untr-dot" title="${esc(D.untranslatedBadge)}">${esc(D.untranslatedShort)}</span>` : '';
      return `<li><a href="${esc(ctx.link(it.url))}"${cur ? ' aria-current="page"' : ''}><span>${esc(it.title)}</span>${beta}${raw}</a></li>`;
    }).join('');
    return `<details class="doc-tree-cat"${isCurrentCat ? ' open' : ''}>
   <summary>${icon(c.icon || 'block')}<span>${esc(c.title)}</span></summary>
   <ul>${links}</ul>
  </details>`;
  }).join('\n');

  return `<aside class="doc-side">
 <details class="doc-tree-box card" open>
  <summary class="doc-tree-mobile-bar"><span>${esc(D.treeTitle)}</span><small>${esc(D.treeToggleHint)}</small></summary>
  <div class="doc-tree-inner">
   ${searchHtml}
   <nav class="doc-tree" aria-label="${esc(D.treeAria)}">
    <div class="doc-tree-head"><b>${esc(D.treeTitle)}</b><a href="${esc(ctx.href('docs'))}">${esc(D.allDocs)}</a></div>
    ${cats}
    <div class="doc-tree-rel"><a href="${esc(ctx.href('releases'))}">${esc(D.releasesLink)} →</a></div>
   </nav>
  </div>
 </details>
</aside>`;
}
