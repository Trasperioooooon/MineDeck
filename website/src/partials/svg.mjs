// SVG 组件接口。图标来自 src/svg/icons.svg（按 id 内联）；插图（A1、A6、A9）各一个文件，{{key}} 由调用方传入的文字替换。
// 动画全部是 CSS（见 css/motion.css），默认画面就是最终帧；prefers-reduced-motion 时全部关闭。
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { esc } from './util.mjs';

const DIR = join(dirname(fileURLToPath(import.meta.url)), '../svg');
const ICONS = {};
for (const m of readFileSync(join(DIR, 'icons.svg'), 'utf8').matchAll(/<symbol id="([\w-]+)" viewBox="([^"]+)">([\s\S]*?)<\/symbol>/g)) ICONS[m[1]] = { box: m[2], body: m[3] };
const ART = {};
const art = (name) => (ART[name] ||= readFileSync(join(DIR, `${name}.svg`), 'utf8').replace(/<!--[\s\S]*?-->\s*/g, ''));

/**
 * 像素图标。
 * @param {string} name icons.svg 里的 id
 * @param {{cls?:string, label?:string}} o 有 label 时作为图片朗读，否则对读屏隐藏
 */
export function icon(name, { cls = '', label } = {}) {
  const i = ICONS[name];
  if (!i) throw new Error(`未知图标 ${name}`);
  const a11y = label ? `role="img" aria-label="${esc(label)}"` : 'aria-hidden="true" focusable="false"';
  return `<svg class="icon icon-${name}${cls ? ' ' + cls : ''}" viewBox="${i.box}" ${a11y} shape-rendering="crispEdges">${i.body}</svg>`;
}

/**
 * 插图。
 * @param {'hero'|'smartscreen'|'broken-block'} name
 * @param {{label:string, text?:Record<string,string>, attrs?:string}} o label 为整张图的替代文字
 */
export function illustration(name, { label, text = {}, attrs = '' }) {
  return art(name)
    .replace('<svg ', `<svg role="img" aria-label="${esc(label)}" ${attrs} `)
    .replace(/\{\{(\w+)\}\}/g, (m, k) => esc(text[k] ?? ''));
}
