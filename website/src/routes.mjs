// 路由表：每个页面声明一次，构建时按 LANGS 自动生成各语言版本。新增页面 = 这里加一行 + pages/ 下一个文件。
//   path     站内路径（中文版），英文版自动加 /en 前缀；以 / 结尾的输出为 目录/index.html
//   params   动态路由：返回参数列表，path 中的 {name} 会被替换
//   section  顶栏高亮哪一项（默认等于 id）
//   scripts  除 core.js 外要加载的脚本
//   langs    只生成哪些语言（默认全部）；absolute 链接写绝对路径；noAlt 不输出 hreflang；noSitemap 不进 sitemap
import home from './pages/home.mjs';
import download from './pages/download.mjs';
import features from './pages/features.mjs';
import releases from './pages/releases.mjs';
import release from './pages/release.mjs';
import help from './pages/help.mjs';
import feedback from './pages/feedback.mjs';
import about from './pages/about.mjs';
import notFound from './pages/not-found.mjs';
import redirect from './pages/redirect.mjs';

export const LANGS = { zh: '', en: '/en' };

export const ROUTES = [
  { id: 'home', path: '/', render: home },
  { id: 'download', path: '/download/', render: download },
  { id: 'features', path: '/features/', render: features },
  { id: 'releases', path: '/releases/', render: releases, scripts: ['releases.js'] },
  { id: 'release', path: '/releases/{tag}/', section: 'releases', params: (data) => data.releases.map((r) => ({ tag: r.tag })), render: release },
  { id: 'help', path: '/help/', render: help },
  { id: 'feedback', path: '/feedback/', render: feedback, scripts: ['feedback.js'] },
  { id: 'about', path: '/about/', render: about },
  // 单页版的旧地址，跳到 /releases/
  { id: 'changelog', path: '/changelog/', render: redirect('releases'), noSitemap: true },
  { id: '404', path: '/404.html', render: notFound, langs: ['zh'], absolute: true, noAlt: true, noSitemap: true },
];

const repo = (cfg) => `https://github.com/${cfg.repo}`;

// 顶栏主导航（站内）。GitHub、语言、下载按钮在 nav.mjs 里固定输出。
export const NAV = ['download', 'features', 'releases', 'help', 'feedback'];

// 页脚分组：route 为站内页面，hash 为页内锚点，ext 为外站（会标 ↗）
export const FOOTER = [
  { h: 'product', links: [{ route: 'download', label: 'nav.download' }, { route: 'features', label: 'nav.features' }, { route: 'releases', label: 'nav.releases' }, { file: 'feed.xml', label: 'footer.feed' }] },
  { h: 'support', links: [{ route: 'help', label: 'nav.help' }, { route: 'feedback', label: 'nav.feedback' }, { route: 'help', hash: 'limits', label: 'footer.knownIssues' }] },
  { h: 'project', links: [{ route: 'about', label: 'nav.about' }, { route: 'about', hash: 'contributors', label: 'footer.contributors' }, { route: 'about', hash: 'license', label: 'footer.license' }, { ext: repo, label: 'footer.source' }] },
];
export { repo };
