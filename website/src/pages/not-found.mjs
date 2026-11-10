// 404（ARCH 4.9）：缺了一块的方块（A9）+ 首页、下载、更新日志三个入口。中英文同页，链接用绝对路径。
import { esc } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { illustration } from '../partials/svg.mjs';

export default function notFound(ctx) {
  const { L, L2: Z } = ctx;
  const links = (l, T) => `<a class="btn" href="${ctx.href('home', {}, l)}"><span>${esc(T.notFound.back)}</span></a><a class="btn" href="${ctx.href('download', {}, l)}"><span>${esc(T.notFound.download)}</span></a><a class="btn" href="${ctx.href('releases', {}, l)}"><span>${esc(T.notFound.releases)}</span></a>`;
  const main = `${illustration('broken-block', { label: L.notFound.aria })}
 <h1>404</h1>
 <p>${esc(L.notFound.p)} <span lang="en">${esc(Z.notFound.p)}</span></p>
 <p class="cta nf-links">${links('zh', L)}</p>
 <p class="cta nf-links" lang="en">${links('en', Z)}</p>`;
  return layout(ctx, { title: L.notFound.title + ' · MineDeck', desc: L.site.description, main, mainClass: 'nf' });
}
