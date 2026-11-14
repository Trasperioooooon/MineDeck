// 旧地址跳转页：meta refresh + canonical，无 JS 也能跳。
import { esc } from '../partials/util.mjs';

export default (to) => (ctx) => {
  const target = ctx.href(to);
  const abs = ctx.cfg.siteUrl.replace(/\/$/, '') + ctx.page.urls[ctx.lang].replace(/changelog\/$/, 'releases/');
  return `<!DOCTYPE html><html lang="${ctx.L.lang}"><head><meta charset="utf-8"><meta name="robots" content="noindex"><meta http-equiv="refresh" content="0; url=${esc(target)}"><link rel="canonical" href="${esc(abs)}"><title>MineDeck</title></head><body><p><a href="${esc(target)}">${esc(ctx.L.nav.releases)}</a></p></body></html>\n`;
};
