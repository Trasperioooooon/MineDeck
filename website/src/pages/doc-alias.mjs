// 文档 aliases 旧路径跳转页：无 JS 也能跳，noSitemap + noindex。
import { esc } from '../partials/util.mjs';

export function renderDocAlias(ctx, alias) {
  const target = ctx.link(alias.targetUrl);
  const abs = ctx.cfg.siteUrl.replace(/\/$/, '') + alias.targetUrl;
  return `<!DOCTYPE html><html lang="${ctx.L.lang}"><head><meta charset="utf-8"><meta name="robots" content="noindex"><meta http-equiv="refresh" content="0; url=${esc(target)}"><link rel="canonical" href="${esc(abs)}"><title>${esc(alias.title)} · MineDeck</title></head><body><p><a href="${esc(target)}">${esc(alias.title)}</a></p></body></html>\n`;
}
