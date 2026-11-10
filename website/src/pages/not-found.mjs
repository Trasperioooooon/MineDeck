import { esc } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';

// 404 同时给出中英文，链接用绝对路径（OSS 会在任意目录下返回它）
export default function notFound(ctx) {
  const { L, L2: Z } = ctx;
  const main = ` <h1>404</h1>
 <p>${esc(L.notFound.p)} <span lang="en">${esc(Z.notFound.p)}</span></p>
 <p><a class="btn" href="${ctx.href('home', {}, 'zh')}">${esc(L.notFound.back)}</a> <a class="btn" href="${ctx.href('home', {}, 'en')}">${esc(Z.notFound.back)}</a></p>`;
  return layout(ctx, { title: L.notFound.title + ' · MineDeck', desc: L.description, main, mainClass: 'nf' });
}
