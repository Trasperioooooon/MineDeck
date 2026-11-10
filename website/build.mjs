#!/usr/bin/env node
// MineDeck 官网构建脚本。只用 Node 自带模块（需要 Node 18 及以上，用到全局 fetch）。
// 字体子集化调用 fonttools 的 pyftsubset（可选）；没有安装时直接复制完整字体并给出提示。
//
// 用法：
//   node build.mjs              联网拉取 GitHub Releases，更新 data/releases.cache.json，输出到 dist/
//   node build.mjs --offline    不联网，用 data/releases.cache.json 构建
//   node build.mjs --preview    链接写成 xxx/index.html，便于双击本地打开（默认输出到 dist-preview/）
//   node build.mjs --out <dir>  指定输出目录
// 环境变量：GITHUB_TOKEN 或 GH_TOKEN（可选，避免匿名限流）；SOURCE_DATE_EPOCH（可选，固定构建时间）

import { readFileSync, writeFileSync, mkdirSync, rmSync, copyFileSync, existsSync, readdirSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { dirname, join, resolve, basename, extname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
import { render, sanitize, summary } from './lib/markdown.mjs';
import { ROUTES, LANGS, NAV, FOOTER_LINKS } from './src/routes.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(HERE, '..');
const args = process.argv.slice(2);
const flag = (n) => args.includes(n);
const opt = (n, d) => { const i = args.indexOf(n); return i >= 0 && args[i + 1] ? args[i + 1] : d; };
const OFFLINE = flag('--offline');
const PREVIEW = flag('--preview');
const OUT = resolve(HERE, opt('--out', PREVIEW ? 'dist-preview' : 'dist'));
const CACHE = join(HERE, 'data', 'releases.cache.json');
const cfg = JSON.parse(readFileSync(join(HERE, 'site.config.json'), 'utf8'));
const I18N = { zh: JSON.parse(readFileSync(join(HERE, 'src/i18n/zh.json'), 'utf8')), en: JSON.parse(readFileSync(join(HERE, 'src/i18n/en.json'), 'utf8')) };
const TOKENS = JSON.parse(readFileSync(join(HERE, 'src/tokens.json'), 'utf8'));
const log = (...a) => console.log('[website]', ...a);

// ---------- 1. 取得 Releases ----------
async function fetchReleases() {
  const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  const all = [];
  for (let page = 1; page < 20; page++) {
    const url = `https://api.github.com/repos/${cfg.repo}/releases?per_page=100&page=${page}`;
    const res = await fetch(url, { headers: { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'minedeck-website-build', ...(token ? { Authorization: `Bearer ${token}` } : {}) } });
    if (!res.ok) throw new Error(`GitHub API ${res.status}: ${(await res.text()).slice(0, 200)}`);
    const list = await res.json();
    all.push(...list);
    if (list.length < 100) break;
  }
  // 缓存只留构建用得到的字段
  return all.map((r) => ({
    tag_name: r.tag_name, name: r.name, draft: r.draft, prerelease: r.prerelease, published_at: r.published_at, html_url: r.html_url, body: r.body,
    assets: (r.assets || []).map((a) => ({ name: a.name, size: a.size, digest: a.digest || null, browser_download_url: a.browser_download_url })),
  }));
}

let raw;
if (OFFLINE) {
  if (!existsSync(CACHE)) { console.error(`离线模式需要 ${CACHE}，先联网构建一次。`); process.exit(1); }
  raw = JSON.parse(readFileSync(CACHE, 'utf8'));
  log(`离线模式：读取缓存 ${raw.length} 个版本`);
} else {
  try {
    raw = await fetchReleases();
    mkdirSync(dirname(CACHE), { recursive: true });
    writeFileSync(CACHE, JSON.stringify(raw, null, 1) + '\n');
    log(`从 GitHub 拉取 ${raw.length} 个版本，已更新缓存`);
  } catch (e) {
    console.error(`拉取 GitHub Releases 失败：${e.message}\n可以改用 --offline，用缓存构建。`);
    process.exit(1);
  }
}

// ---------- 2. 整理数据 ----------
const sizeLabel = (n) => (n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : Math.max(1, Math.round(n / 1024)) + ' KB');
const off = cfg.timezoneOffsetHours * 3600e3;
const localTime = (iso) => new Date(Date.parse(iso) + off).toISOString().slice(0, 16).replace('T', ' ');
const mirrorUrl = (tag, file) => (cfg.mirror && cfg.mirror.enabled ? cfg.mirror.template.replace('{tag}', encodeURIComponent(tag)).replace('{file}', encodeURIComponent(file)) : null);

const releases = raw
  .filter((r) => !r.draft && r.published_at)
  .sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at))
  .map((r) => {
    const assets = r.assets.map((a) => ({
      name: a.name, size: a.size, size_label: sizeLabel(a.size),
      sha256: a.digest && a.digest.startsWith('sha256:') ? a.digest.slice(7) : null,
      github_url: a.browser_download_url, mirror_url: mirrorUrl(r.tag_name, a.name),
    }));
    const exe = assets.find((a) => /\.exe$/i.test(a.name)) || null;
    return {
      tag: r.tag_name, name: r.name || r.tag_name, prerelease: !!r.prerelease, published_at: r.published_at,
      date_local: localTime(r.published_at), html_url: r.html_url,
      legacy: !!(exe && /^AutoMCHUB/i.test(exe.name)),
      summary: summary(r.body), body_html: sanitize(render(r.body || '')), assets, exe,
    };
  });
const generated_at = process.env.SOURCE_DATE_EPOCH ? new Date(+process.env.SOURCE_DATE_EPOCH * 1000).toISOString() : new Date().toISOString();
const pick = (r) => r && { tag: r.tag, published_at: r.published_at, exe: r.exe };
const data = {
  generated_at, repo: cfg.repo, timezone: `UTC+${cfg.timezoneOffsetHours}`, mirror_placeholder: !!(cfg.mirror && cfg.mirror.enabled),
  latest_stable: releases.find((r) => !r.prerelease) || null,
  latest_beta: releases.find((r) => r.prerelease) || null,
  releases,
};

// ---------- 3. 输出目录与静态资源 ----------
rmSync(OUT, { recursive: true, force: true });
mkdirSync(join(OUT, 'assets'), { recursive: true });
mkdirSync(join(OUT, 'data'), { recursive: true });
const assetMap = {}; // 逻辑名 -> 带 hash 的文件名
const hash = (buf) => createHash('sha256').update(buf).digest('hex').slice(0, 10);
function emit(logical, buf) {
  const ext = extname(logical), base = basename(logical, ext);
  const name = `${base}.${hash(buf)}${ext}`;
  writeFileSync(join(OUT, 'assets', name), buf);
  assetMap[logical] = name;
  return name;
}
const SHOTS = I18N.zh.shots.items.map((s) => s.f);
for (const f of SHOTS) emit(f, readFileSync(join(REPO_ROOT, 'docs/images', f)));
emit('logo-144.png', readFileSync(join(REPO_ROOT, 'internal/web/ui/img/logo-144.png')));
emit('social-preview.png', readFileSync(join(REPO_ROOT, 'assets/social-preview.png')));
copyFileSync(join(REPO_ROOT, 'assets/icon.ico'), join(OUT, 'favicon.ico'));

// ---------- 路由 -> 页面列表 ----------
const outFile = (url) => (url.endsWith('/') ? url + 'index.html' : url).replace(/^\//, '');
function urlOf(route, lang, params = {}) {
  const p = route.path.replace(/\{(\w+)\}/g, (m, k) => encodeURIComponent(params[k]));
  return LANGS[lang] + p;
}
const ROUTE_BY_ID = Object.fromEntries(ROUTES.map((r) => [r.id, r]));
const PAGES = [];
for (const route of ROUTES) {
  const langs = route.langs || Object.keys(LANGS);
  for (const params of route.params ? route.params(data) : [{}]) {
    const urls = Object.fromEntries(Object.keys(LANGS).map((l) => [l, urlOf(route, l, params)]));
    for (const lang of langs) {
      PAGES.push({ id: route.id, section: route.section || route.id, lang, params, url: urls[lang], urls, out: outFile(urls[lang]),
        absolute: !!route.absolute, noAlt: !!route.noAlt, noSitemap: !!route.noSitemap, render: route.render });
    }
  }
}

// 先用占位名渲染一遍页面，收集字符，再做字体子集
function makeCtx(page, names) {
  const lang = page.lang;
  const L = I18N[lang];
  const depth = page.out.split('/').length - 1;
  const root = page.absolute ? '/' : '../'.repeat(depth);
  const link = (p) => {
    if (page.absolute) return p;
    let r = root + p.replace(/^\//, '');
    if (r === '') r = './';
    if (PREVIEW && r.endsWith('/')) r += 'index.html';
    return r;
  };
  // href('changelog') / href('release', { tag }) / href('home', {}, 'en')：站内链接一律经由路由表生成
  const href = (id, params = {}, l = lang) => {
    const route = ROUTE_BY_ID[id];
    if (!route) throw new Error(`未知路由 ${id}`);
    return link(urlOf(route, l, params));
  };
  return {
    preview: PREVIEW, L, L2: I18N[lang === 'zh' ? 'en' : 'zh'], cfg, data, tokens: TOKENS, page, root, link, href,
    navItems: NAV.filter((n) => !n.route || ROUTE_BY_ID[n.route]), footerLinks: FOOTER_LINKS,
    a: (n) => root + 'assets/' + (names[n] || n), assetName: (n) => names[n] || n,
  };
}
const draft = PAGES.map((p) => p.render(makeCtx(p, assetMap))).join('');

// ---------- 4. 字体 ----------
const FONT_SRC = {
  pixel: join(HERE, 'fonts/src/fusion-pixel-12px-proportional-zh_hans.otf.woff2'),
  noto: join(REPO_ROOT, 'internal/web/ui/fonts/NotoSansSC-gb2312.woff2'),
  geist: join(REPO_ROOT, 'internal/web/ui/fonts/Geist-latin.woff2'),
  geistmono: join(REPO_ROOT, 'internal/web/ui/fonts/GeistMono-latin.woff2'),
};
const ascii = Array.from({ length: 95 }, (_, i) => String.fromCharCode(32 + i)).join('');
const textOf = (o) => (typeof o === 'string' ? o : Array.isArray(o) ? o.map(textOf).join('') : o && typeof o === 'object' ? Object.values(o).map(textOf).join('') : '');
// 像素字体只用于标题、按钮、版本号等，字符取文案和版本号；正文字体取所有页面文字
const pixelChars = new Set(ascii + textOf(I18N) + releases.map((r) => r.tag).join('') + '0123456789');
const notoChars = new Set(ascii + draft.replace(/<[^>]*>/g, '') + textOf(I18N) + '，。、：；！？「」（）—…·→');
const hasSubset = spawnSync('pyftsubset', ['--help'], { stdio: 'ignore' }).status === 0;
if (!hasSubset) log('未找到 pyftsubset（pip install fonttools brotli），字体不做子集，体积会大很多。');
function subset(key, chars) {
  const src = FONT_SRC[key];
  if (!hasSubset) return readFileSync(src);
  const tmp = join(tmpdir(), `md-${key}-${process.pid}`);
  writeFileSync(tmp + '.txt', [...chars].join(''));
  const r = spawnSync('pyftsubset', [src, `--text-file=${tmp}.txt`, '--flavor=woff2', `--output-file=${tmp}.woff2`, '--layout-features=*', '--no-hinting'], { stdio: 'inherit' });
  if (r.status !== 0) { log(`子集化 ${key} 失败，改用完整字体`); return readFileSync(src); }
  return readFileSync(tmp + '.woff2');
}
emit('pixel.woff2', subset('pixel', pixelChars));
emit('noto.woff2', subset('noto', notoChars));
emit('geist.woff2', readFileSync(FONT_SRC.geist));
emit('geistmono.woff2', readFileSync(FONT_SRC.geistmono));
// 字体许可证随站点发布
mkdirSync(join(OUT, 'assets/licenses'), { recursive: true });
for (const f of ['OFL-Geist.txt', 'OFL-NotoSansSC.txt']) copyFileSync(join(REPO_ROOT, 'internal/web/ui/fonts', f), join(OUT, 'assets/licenses', f));
for (const f of readdirSync(join(HERE, 'fonts/licenses'))) copyFileSync(join(HERE, 'fonts/licenses', f), join(OUT, 'assets/licenses', f));

// ---------- 5. CSS / JS ----------
// 设计 token：src/tokens.json -> CSS 变量；组件样式不得出现颜色字面量
function tokensCss(t) {
  const out = [];
  for (const [group, vals] of Object.entries(t)) {
    if (group.startsWith('_') || group === 'breakpoint') continue;
    if (Array.isArray(vals)) vals.forEach((v, i) => out.push(`--${group}-${i}:${v}`));
    else for (const [k, v] of Object.entries(vals)) out.push(`--${group}-${k}:${v}`);
  }
  return `:root{${out.join(';')}}\n`;
}
const STYLE_FILES = ['base.css', 'components.css', 'pages.css'];
let css = tokensCss(TOKENS);
for (const f of STYLE_FILES) {
  const src = readFileSync(join(HERE, 'src/styles', f), 'utf8');
  const bad = src.replace(/\/\*[\s\S]*?\*\//g, '').match(/#[0-9a-fA-F]{3,8}\b|rgba?\(/g);
  if (bad) { console.error(`src/styles/${f} 里有颜色字面量 ${bad.join(' ')}，请改用 tokens.json`); process.exit(1); }
  css += src + '\n';
}
css = css.replace(/\{\{font:(\w+)\}\}/g, (m, k) => assetMap[`${k}.woff2`]);
emit('style.css', Buffer.from(css));
emit('app.js', readFileSync(join(HERE, 'src/app.js')));

// ---------- 6. 页面与数据 ----------
for (const p of PAGES) {
  const html = p.render(makeCtx(p, assetMap));
  mkdirSync(dirname(join(OUT, p.out)), { recursive: true });
  writeFileSync(join(OUT, p.out), html);
}
writeFileSync(join(OUT, 'data/releases.json'), JSON.stringify({ ...data, latest_stable: pick(data.latest_stable), latest_beta: pick(data.latest_beta) }));
const site = cfg.siteUrl.replace(/\/$/, '');
writeFileSync(join(OUT, 'robots.txt'), `User-agent: *\nAllow: /\nSitemap: ${site}/sitemap.xml\n`);
writeFileSync(join(OUT, 'sitemap.xml'), `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">\n` +
  PAGES.filter((p) => !p.noSitemap).map((p) => `<url><loc>${site}${p.url}</loc><xhtml:link rel="alternate" hreflang="zh-CN" href="${site}${p.urls.zh}"/><xhtml:link rel="alternate" hreflang="en" href="${site}${p.urls.en}"/></url>`).join('\n') + '\n</urlset>\n');

log(`完成：${OUT}`);
log(`稳定版 ${data.latest_stable?.tag ?? '无'}，测试版 ${data.latest_beta?.tag ?? '无'}，共 ${releases.length} 个版本${cfg.mirror?.enabled ? '；镜像链接为占位模板 ' + cfg.mirror.template : ''}`);
for (const [k, v] of Object.entries(assetMap)) if (k.endsWith('.woff2')) log(`  ${v}  ${(readFileSync(join(OUT, 'assets', v)).length / 1024).toFixed(0)} KB`);
