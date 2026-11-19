// 文档搜索框组件（ARIA combobox + 无 JS 降级提示 + --preview file:// 兼容属性）。
import { esc } from './util.mjs';
import { icon } from './svg.mjs';

export function docSearch(ctx, { variant = 'hero', initialQuery = '', idPrefix = 'doc-s' } = {}) {
  const { L, lang, root, a, preview } = ctx;
  const D = L.docs;
  const docsBase = ctx.link(lang === 'zh' ? '/docs/' : '/en/docs/').replace(/index\.html$/, '');
  const listId = `${idPrefix}-list`;
  const inputId = `${idPrefix}-in`;
  const cls = variant === 'hero' ? 'doc-search card is-hero' : 'doc-search is-compact';
  return `<div class="${cls}" data-doc-search
  data-lang="${esc(lang)}"
  data-docs-root="${esc(docsBase)}"
  data-preview="${preview ? '1' : ''}"
  data-index-json="${esc(root + `data/docs-index.${lang}.json`)}"
  data-index-js="${esc(a(`docs-index.${lang}.js`))}"
  data-empty="${esc(D.searchEmpty)}"
  data-loading="${esc(D.searchLoading)}"
  data-error="${esc(D.searchError)}"
  data-count="${esc(D.searchCount)}"
  data-zh-badge="${esc(D.searchZhBadge || '')}">
 <div class="doc-search-row js-only">
  <span class="doc-search-ico" aria-hidden="true">${icon('search')}</span>
  <input id="${esc(inputId)}" class="doc-search-in" type="search"
   role="combobox" aria-autocomplete="list" aria-expanded="false"
   aria-controls="${esc(listId)}" aria-activedescendant=""
   aria-label="${esc(D.searchAria)}"
   placeholder="${esc(variant === 'hero' ? D.searchPlaceholder : D.searchShortPlaceholder)}"
   value="${esc(initialQuery)}" autocomplete="off" spellcheck="false">
  <button type="button" class="doc-search-clear" aria-label="${esc(D.searchClear)}" hidden>×</button>
  <kbd class="doc-kbd" aria-hidden="true">${variant === 'hero' ? 'Ctrl+K / /' : '/'}</kbd>
 </div>
 <noscript><p class="doc-nojs-note muted">${esc(D.searchNoJs)}</p></noscript>
 <div id="${esc(listId)}" class="doc-search-pop card" role="listbox" aria-label="${esc(D.searchResultsAria)}" hidden></div>
 <div class="sr" aria-live="polite" data-search-status></div>
</div>`;
}
