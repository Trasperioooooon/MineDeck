// 模板里用到的两个小工具：HTML 转义与 {占位} 替换。
export const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
export const fmt = (s, v = {}) => String(s).replace(/\{(\w+)\}/g, (m, k) => (k in v ? v[k] : m));
/** 拼属性：attrs({ id: 'x', hidden: true, title: null }) -> ' id="x" hidden' */
export const attrs = (o) => Object.entries(o).map(([k, v]) => (v === true ? ` ${k}` : v == null || v === false ? '' : ` ${k}="${esc(v)}"`)).join('');
/** 英文页里出现的中文内容（Release 说明）标注 lang，便于读屏和字体回退 */
export const zhAttr = (ctx, when = true) => (ctx.page.lang === 'en' && when ? ' lang="zh-CN"' : '');
