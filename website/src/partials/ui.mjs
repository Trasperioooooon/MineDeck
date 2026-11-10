// 通用 UI 组件。只输出 class，外观全部在 styles/components.css 里由 token 决定。
import { esc, attrs } from './util.mjs';

/**
 * 按钮链接。
 * @param {{href:string,label:string,sub?:string,primary?:boolean,size?:'sm',pix?:boolean,ext?:boolean,cls?:string}} o
 *  ext=true 表示外站（GitHub 等），会加 .ext 标记并以 ↗ 提示。
 */
export function button({ href, label, sub, primary, size, pix, ext, cls, raw }) {
  const c = ['btn', size, primary && 'primary', ext && 'ext', cls].filter(Boolean).join(' ');
  const text = raw ? label : esc(label);
  const inner = sub != null ? `<span${pix ? ' class="pix"' : ''}>${text}</span><small>${esc(sub)}</small>` : (pix ? `<span class="pix">${text}</span>` : size ? text : `<span>${text}</span>`);
  return `<a class="${c}" href="${esc(href)}">${inner}</a>`;
}

/** 卡片容器。tag 默认 div；cls 追加在 card 之后。 */
export const card = (body, { tag = 'div', cls = '', ...rest } = {}) => `<${tag} class="card${cls ? ' ' + cls : ''}"${attrs(rest)}>${body}</${tag}>`;

/** 版本徽章：稳定版 / 测试版 */
export const versionBadge = (L, r) => (r.prerelease ? `<span class="badge bt">${esc(L.log.betaF)}</span>` : `<span class="badge st">${esc(L.log.stable)}</span>`);

/** 区块标题。level 1 用于页面主标题。 */
export const secHead = ({ h, p, level = 2, more }) =>
  `<div class="sec-h"><h${level}>${esc(h)}</h${level}>${p ? `<p>${p}</p>` : ''}${more || ''}</div>`;

/** 面包屑：items = [{label, href?}]，最后一项为当前页 */
export const crumbs = (ctx, items) => `<nav class="crumbs" aria-label="${esc(ctx.L.crumbsAria)}"><ol>${items.map((it, i) =>
  i === items.length - 1 ? `<li><span aria-current="page">${esc(it.label)}</span></li>` : `<li><a href="${esc(it.href)}">${esc(it.label)}</a></li>`).join('')}</ol></nav>`;

/**
 * 外站链接（GitHub 等）。站内有同等内容时应优先给站内链接，外链标「备用」。
 * @param {{fallback?:boolean, cls?:string}} o fallback=true 时文字后追加「（备用）」标记
 */
export const extLink = (ctx, href, label, { fallback, cls } = {}) =>
  `<a class="${['ext', cls].filter(Boolean).join(' ')}" href="${esc(href)}" rel="noopener">${esc(label)}${fallback ? ` <span class="fallback">${esc(ctx.L.fallback)}</span>` : ''}</a>`;

/** 功能卡片。i 决定图标色块取 tokens.palette 的第几个颜色。 */
export const PALETTE_SIZE = 12; // 与 tokens.json 的 palette 长度一致
export const featCard = (it, i, badge) =>
  `<div class="card feat"><div class="ico" style="--c:var(--palette-${i % PALETTE_SIZE})" aria-hidden="true"></div><h3>${esc(it.t)}${badge ? ` <span class="badge bt">${esc(badge)}</span>` : ''}</h3><p>${esc(it.d)}</p></div>`;
