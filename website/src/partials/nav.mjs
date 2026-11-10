// 顶栏（ARCH 3.1）：品牌、主导航、GitHub 图标（外链）、语言切换、「下载 v{稳定版}」按钮。
// 窄屏把导航收进 <details> 菜单，无 JS 也能展开。当前栏目加 aria-current="page"。
import { esc, fmt } from './util.mjs';
import { icon } from './svg.mjs';
import { repo } from '../routes.mjs';

export function nav(ctx) {
  const { L, page, data } = ctx;
  const links = ctx.nav.map((id) => `<a href="${ctx.href(id)}"${page.section === id ? ' aria-current="page"' : ''}>${esc(L.nav[id])}</a>`).join('');
  const gh = `<a class="gh-ico" href="${repo(ctx.cfg)}" data-ext target="_blank" rel="noopener" aria-label="${esc(L.nav.githubAria)}" title="${esc(L.nav.githubAria)}">${icon('code')}<span class="gh-txt">${esc(L.nav.github)}</span></a>`;
  const other = page.lang === 'zh' ? 'en' : 'zh';
  const lang = page.noAlt ? '' : `<span class="lang" role="group" aria-label="${esc(L.langSwitchAria)}">${page.lang === 'zh'
    ? `<span aria-current="true">中</span><a href="${ctx.link(page.urls[other])}" hreflang="en" lang="en" data-set-lang="en">EN</a>`
    : `<a href="${ctx.link(page.urls[other])}" hreflang="zh-CN" lang="zh-CN" data-set-lang="zh">中</a><span aria-current="true">EN</span>`}</span>`;
  const st = data.latest_stable;
  const dl = `<a class="btn sm primary nav-dl" href="${ctx.href('download')}"${page.section === 'download' ? ' aria-current="page"' : ''}>${esc(st ? fmt(L.nav.downloadBtn, { version: st.tag }) : L.nav.download)}</a>`;
  return `<div class="wrap"><header class="top">
 <a class="brand" href="${ctx.href('home')}"><img src="${ctx.a('logo-144.png')}" alt="" width="40" height="40"><span>MineDeck</span></a>
 <nav class="nav nav-main" aria-label="MineDeck">${links}</nav>
 <div class="top-r">${gh}${lang}${dl}</div>
 <details class="menu">
  <summary aria-label="${esc(L.menu)}">${icon('menu')}<span>${esc(L.menu)}</span></summary>
  <div class="menu-panel card"><nav class="nav nav-menu" aria-label="MineDeck">${links}</nav><div class="menu-row">${gh}${lang}</div></div>
 </details>
</header></div>`;
}
