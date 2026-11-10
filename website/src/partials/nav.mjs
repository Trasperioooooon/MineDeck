// 顶栏：品牌、主导航（来自 routes.mjs 的 NAV）、语言切换。当前栏目加 aria-current。
import { esc } from './util.mjs';

export function nav(ctx) {
  const { L, page } = ctx;
  const items = ctx.navItems.map((n) => {
    const cur = n.route && n.route === page.section && !n.hash;
    const href = n.ext ? n.ext(ctx.cfg) : ctx.href(n.route) + (n.hash ? '#' + n.hash : '');
    // 首页里的锚点不必绕回首页
    const h = n.hash && page.id === n.route ? '#' + n.hash : href;
    const cls = [n.opt && 'opt', n.ext && 'ext-nav'].filter(Boolean).join(' ');
    return `<a${cls ? ` class="${cls}"` : ''} href="${esc(h)}"${cur ? ' aria-current="page"' : ''}>${esc(L.nav[n.label])}</a>`;
  }).join('\n  ');
  const other = page.lang === 'zh' ? 'en' : 'zh';
  const lang = page.noAlt ? '' : `<span class="lang" role="group" aria-label="${esc(L.langSwitchAria)}">${page.lang === 'zh'
    ? `<span aria-current="true">中</span><a href="${ctx.link(page.urls[other])}" hreflang="en" lang="en" data-set-lang="en">EN</a>`
    : `<a href="${ctx.link(page.urls[other])}" hreflang="zh-CN" lang="zh-CN" data-set-lang="zh">中</a><span aria-current="true">EN</span>`}</span>`;
  return `<div class="wrap"><header class="top">
 <a class="brand" href="${ctx.href('home')}"><img src="${ctx.a('logo-144.png')}" alt="" width="40" height="40"><span>MineDeck</span></a>
 <nav class="nav" aria-label="MineDeck">
  ${items}
  ${lang}
 </nav>
</header></div>`;
}
