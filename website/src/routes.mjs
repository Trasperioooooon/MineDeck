// 路由表：每个页面声明一次，构建时按 LANGS 自动生成各语言版本。
//   path     站内路径（中文版），英文版自动加 /en 前缀；以 / 结尾的输出为 目录/index.html
//   params   动态路由：返回参数列表，path 中的 {name} 会被替换
//   section  顶栏高亮哪一项（默认等于 id）
//   langs    只生成哪些语言（默认全部）；absolute 链接写绝对路径；noAlt 不输出 hreflang
import home from './pages/home.mjs';
import changelog from './pages/changelog.mjs';
import notFound from './pages/not-found.mjs';

export const LANGS = { zh: '', en: '/en' };

export const ROUTES = [
  { id: 'home', path: '/', render: home },
  { id: 'changelog', path: '/changelog/', render: changelog },
  { id: '404', path: '/404.html', render: notFound, langs: ['zh'], absolute: true, noAlt: true, noSitemap: true },
];

// 顶栏：route 指向站内页面；ext 为外站（只作源码入口）。opt=true 的项在窄屏隐藏。
const repo = (cfg) => `https://github.com/${cfg.repo}`;
export const NAV = [
  { route: 'home', hash: 'features', label: 'features', opt: true },
  { route: 'changelog', label: 'changelog' },
  { ext: repo, label: 'github', opt: true },
];
export const FOOTER_LINKS = [
  { ext: repo, text: 'GitHub' },
  { ext: (cfg) => `${repo(cfg)}/issues`, text: 'Issues' },
];
