// 通用 UI 组件。只输出结构和 class，外观由 css/components.css 里的 token 决定。
import { esc, attrs } from './util.mjs';
import { icon } from './svg.mjs';

/**
 * 按钮链接。
 * @param {{href:string,label:string,sub?:string,primary?:boolean,size?:'sm',pix?:boolean,ext?:boolean,cls?:string,icon?:string,raw?:boolean}} o
 *  ext=true：外站（GitHub 等），加 data-ext 和 ↗；构建检查要求所有 GitHub 链接都带 data-ext。
 *  icon='arrow'：下载按钮里的像素箭头，悬停时跳两格（A2）。
 */
export function button({ href, label, sub, primary, size, pix, ext, cls, icon: ic, raw, ctx }) {
  const c = ['btn', size, primary && 'primary', ext && 'ext', cls].filter(Boolean).join(' ');
  const text = raw ? label : esc(label);
  const lead = ic ? icon(ic, { cls: 'btn-ico' }) : '';
  const body = sub != null ? `<span${pix ? ' class="pix"' : ''}>${text}</span><small>${esc(sub)}</small>` : `<span${pix ? ' class="pix"' : ''}>${text}</span>`;
  const extA = ext ? ` data-ext rel="noopener"` : '';
  const sr = ext && ctx ? `<span class="sr">${esc(ctx.L.extSr)}</span>` : '';
  return `<a class="${c}" href="${esc(href)}"${extA}>${lead}${ic ? `<span class="btn-txt">${body}</span>` : body}${sr}</a>`;
}

/**
 * 外站链接（ARCH 3.4）：统一 target=_blank、rel=noopener、↗ 和读屏提示。站内有同等内容时应优先给站内链接。
 * @param {{fallback?:boolean, cls?:string, aria?:string, html?:string}} o fallback=true 时追加「备用」标记
 */
export const extLink = (ctx, href, label, { fallback, cls, aria, html } = {}) =>
  `<a class="${['ext', cls].filter(Boolean).join(' ')}" href="${esc(href)}" data-ext target="_blank" rel="noopener"${aria ? ` aria-label="${esc(aria)}"` : ''}>${html ?? esc(label)}${fallback ? ` <span class="fallback">${esc(ctx.L.fallback)}</span>` : ''}<span class="sr">${esc(ctx.L.extSr)}</span></a>`;

/** 卡片容器 */
export const card = (body, { tag = 'div', cls = '', ...rest } = {}) => `<${tag} class="card${cls ? ' ' + cls : ''}"${attrs(rest)}>${body}</${tag}>`;

/** 版本徽章：稳定版 / 测试版 */
export const versionBadge = (L, r) => (r.prerelease ? `<span class="badge bt">${esc(L.log.betaF)}</span>` : `<span class="badge st">${esc(L.log.stable)}</span>`);

/** 区块标题。p 为已转义的 HTML；more 为右侧链接 HTML */
export const secHead = ({ h, p, level = 2, more, id }) =>
  `<div class="sec-h"><h${level}${id ? ` id="${esc(id)}"` : ''}>${esc(h)}</h${level}>${p ? `<p>${p}</p>` : ''}${more ? `<span class="more-link">${more}</span>` : ''}</div>`;

/** 站内「→」文字链接 */
export const go = (href, label) => `<a class="read-more" href="${esc(href)}">${esc(label)} →</a>`;

/** 二级页页头：面包屑 + 标题 + 导语 */
export function pageHead(ctx, { h, lead, crumbs: cr, extra = '' }) {
  return `<header class="page-head">${cr ? crumbs(ctx, cr) : ''}<h1 class="pix page-title">${esc(h)}</h1>${lead ? `<p class="page-lead">${lead}</p>` : ''}${extra}</header>`;
}

/** 面包屑：items = [{label, href?}]，最后一项为当前页；同时输出 BreadcrumbList JSON-LD */
export function crumbs(ctx, items) {
  const site = ctx.cfg.siteUrl.replace(/\/$/, '');
  const ld = { '@context': 'https://schema.org', '@type': 'BreadcrumbList', itemListElement: items.map((it, i) => ({ '@type': 'ListItem', position: i + 1, name: it.label, item: site + (it.url || ctx.page.url) })) };
  return `<nav class="crumbs" aria-label="${esc(ctx.L.crumbsAria)}"><ol>${items.map((it, i) =>
    i === items.length - 1 ? `<li><span aria-current="page">${esc(it.label)}</span></li>` : `<li><a href="${esc(it.href)}">${esc(it.label)}</a></li>`).join('')}</ol></nav>
<script type="application/ld+json">${JSON.stringify(ld).replace(/</g, '\\u003c')}</script>`;
}

/** 统计格。count=true 时数字由 JS 从 0 计数（A5），无 JS 或减少动效时直接显示 */
export const stat = ({ label, value, href, cls = '', note, count }) => {
  const v = `<b${count ? ` data-count="${esc(value)}"` : ''}>${esc(value)}</b>`;
  const inner = `<small>${esc(label)}</small>${v}${note ? `<span class="stat-note">${esc(note)}</span>` : ''}`;
  return href ? `<a class="card stat ${cls}" href="${esc(href)}">${inner}</a>` : `<div class="card stat ${cls}">${inner}</div>`;
};

/** 可复制的文本（SHA-256、命令等）。无 JS 时按钮隐藏，文本可选中 */
export const copyable = (ctx, text, { short, cls = '', label } = {}) =>
  `<span class="copyable ${cls}"><code title="${esc(text)}">${esc(short ? text.slice(0, 12) + '…' + text.slice(-6) : text)}</code><button type="button" class="copy-btn" data-copy="${esc(text)}" aria-label="${esc(label || ctx.L.copy)}" data-done="${esc(ctx.L.copied)}">${icon('copy', { cls: 'i-copy' })}${icon('check', { cls: 'i-check' })}</button></span>`;

/** 功能卡片（首页 4 张）。图标悬停时有小动画（A3） */
export const featCard = ({ t, d, icon: ic }, { badge, i = 0 } = {}) =>
  `<div class="card feat"><div class="ico" style="--c:var(--icon-${i % 12})">${icon(ic || 'block', { cls: 'feat-ico' })}</div><h3>${esc(t)}${badge ? ` <span class="badge bt">${esc(badge)}</span>` : ''}</h3><p>${esc(d)}</p></div>`;

/** 页内目录 */
export const toc = (title, items) => `<nav class="toc card" aria-label="${esc(title)}"><b>${esc(title)}</b><ol>${items.map((it) => `<li><a href="#${esc(it.id)}">${esc(it.text)}</a></li>`).join('')}</ol></nav>`;
