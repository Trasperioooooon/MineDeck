// 文档文章页（/docs/<cat>/<slug>/）：左分类树 + 中正文章节 + 右本页目录 + 上下篇/更新时间/GitHub 编辑/反馈预填。
import { esc, fmt, zhAttr } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { crumbs, extLink, toc } from '../partials/ui.mjs';
import { repo } from '../routes.mjs';
import { docSearch } from '../partials/doc-search.mjs';
import { docTree } from '../partials/doc-tree.mjs';

export default function docArticle(ctx) {
  const { L, cfg, data, page, lang } = ctx;
  const D = L.docs;
  const docsModel = data.docs;
  const key = `${page.params.cat}/${page.params.slug}`;
  const doc = docsModel.articleByKey[lang].get(key);
  if (!doc) throw new Error(`找不到文档 ${lang}/${key}`);

  const tree = docsModel.treeByLang[lang];
  const side = docTree(ctx, tree, {
    currentCat: doc.cat,
    currentSlug: doc.slug,
    searchHtml: docSearch(ctx, { variant: 'compact', idPrefix: 'doc-side-s' }),
  });

  const badges = [
    doc.since ? `<span class="badge st">${esc(fmt(D.sinceLabel, { version: doc.since }))}</span>` : '',
    doc.beta ? `<span class="badge bt">${esc(D.betaLabel)}</span>` : '',
  ].filter(Boolean).join('');

  const fallbackBanner = doc.untranslated
    ? `<div class="notice doc-fallback" role="note">
   <b>${esc(D.untranslatedTitle)}</b>
   <p>${esc(D.untranslatedBody)}</p>
   <p><a class="read-more" href="${esc(ctx.link(doc.zhUrl))}" hreflang="zh-CN" lang="zh-CN" data-set-lang="zh">${esc(D.untranslatedCta)} →</a></p>
  </div>`
    : '';

  const bodyHtml = doc.untranslated ? '' : docsModel.renderArticle(doc, ctx);
  const headings = doc.untranslated ? [] : doc.parsed.headings.filter((h) => h.level >= 2 && h.level <= 3);
  const hasToc = headings.length >= 2;
  const tocItems = headings.map((h) => ({ id: h.id, text: h.level === 3 ? `· ${h.text}` : h.text }));
  const rightToc = hasToc ? `<aside class="doc-toc-col">${toc(D.toc, tocItems)}</aside>` : '';
  const mobileToc = hasToc
    ? `<details class="toc card doc-toc-inline"><summary><b>${esc(D.toc)}</b></summary><ol>${tocItems.map((it) => `<li><a href="#${esc(it.id)}">${esc(it.text)}</a></li>`).join('')}</ol></details>`
    : '';

  const pagerCard = (item, label, cls) => {
    if (!item) return '<span></span>';
    const titleLang = ctx.lang === 'en' && item.untranslated ? ' lang="zh-CN"' : '';
    return `<a class="card ${cls}" href="${esc(ctx.link(item.url))}"><small>${esc(label)} · ${esc(item.catTitle)}</small><b${titleLang}>${esc(item.title)}</b></a>`;
  };
  const pager = (doc.prev || doc.next)
    ? `<nav class="pager doc-pager" aria-label="${esc(D.pagerAria)}">${pagerCard(doc.prev, D.prevDoc, 'prev')}${pagerCard(doc.next, D.nextDoc, 'next')}</nav>`
    : '';

  const branch = (data.repoInfo && data.repoInfo.default_branch) || 'main';
  const editUrl = doc.untranslated
    ? `${repo(cfg)}/new/${encodeURIComponent(branch)}/website/docs-content/en/${doc.cat}?filename=${encodeURIComponent(doc.slug + '.md')}`
    : `${repo(cfg)}/edit/${encodeURIComponent(branch)}/${doc.repoRel}`;
  const feedbackUrl = `${ctx.href('feedback')}?kind=suggestion&doc=${encodeURIComponent(key)}&title=${encodeURIComponent(doc.title)}`;

  const docCrumbList = [
    ctx.crumb('home', L.crumbHome),
    ctx.crumb('docs', D.h),
    { label: doc.catTitle, href: `${ctx.href('docs')}#cat-${encodeURIComponent(doc.cat)}`, url: `${ctx.lang === 'zh' ? '' : '/en'}/docs/#cat-${encodeURIComponent(doc.cat)}` },
    { label: doc.title, href: ctx.link(doc.url), url: doc.url },
  ];

  const metaLine = (badges || doc.updated)
    ? `<div class="doc-badges">${badges}${doc.updated ? `<span class="doc-updated-top">${esc(fmt(D.updatedAt, { date: doc.updated }))}</span>` : ''}</div>`
    : '';

  const footMeta = `<footer class="doc-foot">
  <div class="doc-foot-meta">${doc.updated ? `<time datetime="${esc(doc.updated)}">${esc(fmt(D.updatedAt, { date: doc.updated }))}</time>` : ''}</div>
  <div class="doc-foot-acts">
   ${extLink(ctx, editUrl, doc.untranslated ? D.translateOnGithub : D.editOnGithub)}
   <a class="read-more" href="${esc(feedbackUrl)}">${esc(D.reportDoc)} →</a>
  </div>
 </footer>`;

  const main = `${crumbs(ctx, docCrumbList)}
<div class="doc-layout${hasToc ? '' : ' no-toc'}">
 ${side}
 <article class="doc-main">
  <header class="doc-head">
   <h1 class="pix page-title"${zhAttr(ctx, doc.untranslated)}>${esc(doc.title)}</h1>
   ${doc.description ? `<p class="page-lead"${zhAttr(ctx, doc.untranslated)}>${esc(doc.description)}</p>` : ''}
   ${metaLine}
  </header>
  ${fallbackBanner}
  ${mobileToc}
  ${bodyHtml ? `<div class="card doc-body doc-prose">${bodyHtml}</div>` : ''}
  ${footMeta}
  ${pager}
 </article>
 ${rightToc}
</div>`;

  return layout(ctx, {
    title: `${doc.title} · ${D.h} · MineDeck`,
    desc: doc.description || D.desc,
    main,
    mainClass: 'wrap-docs',
  });
}
