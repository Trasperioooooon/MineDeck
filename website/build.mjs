#!/usr/bin/env node
// MineDeck 官网构建脚本。只用 Node 自带模块（Node 18+，fetch 步骤用到全局 fetch）。
// 字体子集化调用 fonttools 的 pyftsubset（可选）；头像压缩用 Pillow（可选）。
//
// 用法：
//   node build.mjs fetch        联网：GitHub Releases、仓库信息、贡献者与头像、提交活动 → data/
//   node build.mjs build        离线、确定性：data/ + content/ + src/ → dist/
//   node build.mjs              等于 fetch && build；fetch 失败时用已有 data/ 继续并警告
//   --offline                   同 build（兼容旧用法）
//   --preview                   链接写成 xxx/index.html，便于双击本地打开（默认输出到 dist-preview/）
//   --out <dir>                 指定输出目录
// 环境变量：GITHUB_TOKEN 或 GH_TOKEN（可选）；SOURCE_DATE_EPOCH（可选，固定构建时间）

import { readFileSync, writeFileSync, mkdirSync, rmSync, copyFileSync, existsSync, readdirSync, statSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { dirname, join, resolve, basename, extname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';
import { tmpdir } from 'node:os';
import { fetchAll, hasData, readJson } from './lib/fetch.mjs';
import { buildModel, pickRelease } from './lib/data.mjs';
import { ROUTES, LANGS, NAV, FOOTER } from './src/routes.mjs';
import { checkDist } from './lib/checks.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = resolve(HERE, '..');
const DATA = join(HERE, 'data');
const args = process.argv.slice(2);
const flag = (n) => args.includes(n);
const opt = (n, d) => { const i = args.indexOf(n); return i >= 0 && args[i + 1] ? args[i + 1] : d; };
const cmd = args.find((a) => a === 'fetch' || a === 'build') || (flag('--offline') ? 'build' : 'all');
const PREVIEW = flag('--preview');
const OUT = resolve(HERE, opt('--out', PREVIEW ? 'dist-preview' : 'dist'));
const log = (...a) => console.log('[website]', ...a);
const readText = (p) => readFileSync(p, 'utf8');
const cfg = JSON.parse(readText(join(HERE, 'site.config.json')));
const TOKENS = JSON.parse(readText(join(HERE, 'src/tokens.json')));

// ---------- 1. fetch ----------
if (cmd !== 'build') {
  const ok = await fetchAll({ cfg, dataDir: DATA, repoRoot: REPO_ROOT, log });
  if (cmd === 'fetch') process.exit(ok ? 0 : 1);
}
if (!hasData(DATA)) { console.error('data/releases.cache.json 不存在，先联网运行一次 node build.mjs fetch。'); process.exit(1); }

// ---------- 2. 数据与文案 ----------
const generatedAt = process.env.SOURCE_DATE_EPOCH ? new Date(+process.env.SOURCE_DATE_EPOCH * 1000).toISOString() : new Date().toISOString();
const data = buildModel({
  raw: readJson(DATA, 'releases.cache.json'), repoInfo: readJson(DATA, 'repo.json', null),
  contributors: readJson(DATA, 'contributors.json', null), activity: readJson(DATA, 'activity.json', null), cfg, generatedAt,
});
data.license = existsSync(join(REPO_ROOT, 'LICENSE')) ? readText(join(REPO_ROOT, 'LICENSE')) : '';
const CONTENT = {};
for (const lang of Object.keys(LANGS)) {
  CONTENT[lang] = {};
  for (const f of readdirSync(join(HERE, 'content'))) {
    const m = f.match(/^(\w+)\.(\w+)\.json$/);
    if (m && m[2] === lang) CONTENT[lang][m[1]] = JSON.parse(readText(join(HERE, 'content', f)));
  }
}
log(`数据：${data.releases.length} 个版本，稳定版 ${data.latest_stable?.tag ?? '无'}，测试版 ${data.latest_beta?.tag ?? '无'}，贡献者 ${data.contributors.list.length} 位（隐藏 ${data.contributors.hidden}）`);

// ---------- 3. 输出目录与静态资源 ----------
rmSync(OUT, { recursive: true, force: true });
mkdirSync(join(OUT, 'assets'), { recursive: true });
mkdirSync(join(OUT, 'data'), { recursive: true });
const assetMap = {}; // 逻辑名 -> 带 hash 的文件名（相对 assets/）
const hash = (buf) => createHash('sha256').update(buf).digest('hex').slice(0, 10);
function emit(logical, buf) {
  const ext = extname(logical), base = logical.slice(0, -ext.length);
  const name = `${base}.${hash(buf)}${ext}`;
  mkdirSync(dirname(join(OUT, 'assets', name)), { recursive: true });
  writeFileSync(join(OUT, 'assets', name), buf);
  assetMap[logical] = name;
  return name;
}
for (const s of CONTENT.zh.ui.shots.items) emit(s.f, readFileSync(join(REPO_ROOT, 'docs/images', s.f)));
emit('logo-144.png', readFileSync(join(REPO_ROOT, 'internal/web/ui/img/logo-144.png')));
emit('social-preview.png', readFileSync(join(REPO_ROOT, 'assets/social-preview.png')));
copyFileSync(join(REPO_ROOT, 'assets/icon.ico'), join(OUT, 'favicon.ico'));
for (const c of data.contributors.list) {
  const f = c.avatar && join(DATA, c.avatar);
  c.avatarAsset = f && existsSync(f) ? emit(`avatars/${c.login}.png`, readFileSync(f)) && `avatars/${c.login}.png` : null;
}

// ---------- 4. 路由 -> 页面列表 ----------
const outFile = (url) => (url.endsWith('/') ? url + 'index.html' : url).replace(/^\//, '');
const urlOf = (route, lang, params = {}) => LANGS[lang] + route.path.replace(/\{(\w+)\}/g, (m, k) => encodeURIComponent(params[k]));
const ROUTE_BY_ID = Object.fromEntries(ROUTES.map((r) => [r.id, r]));
const PAGES = [];
for (const route of ROUTES) {
  const langs = route.langs || Object.keys(LANGS);
  for (const params of route.params ? route.params(data) : [{}]) {
    const urls = Object.fromEntries(Object.keys(LANGS).map((l) => [l, urlOf(route, l, params)]));
    for (const lang of langs) {
      PAGES.push({ id: route.id, section: route.section || route.id, lang, params, url: urls[lang], urls, out: outFile(urls[lang]),
        absolute: !!route.absolute, noAlt: !!route.noAlt, noSitemap: !!route.noSitemap, scripts: route.scripts || [], render: route.render });
    }
  }
}

function makeCtx(page, names) {
  const lang = page.lang;
  const C = CONTENT[lang];
  const depth = page.out.split('/').length - 1;
  const root = page.absolute ? '/' : '../'.repeat(depth);
  const link = (p) => {
    if (page.absolute) return p;
    let r = root + p.replace(/^\//, '');
    if (r === '') r = './';
    if (PREVIEW && r.endsWith('/')) r += 'index.html';
    return r;
  };
  // 站内链接一律经由路由表：href('releases') / href('release', { tag }) / href('home', {}, 'en')
  const href = (id, params = {}, l = lang) => {
    const route = ROUTE_BY_ID[id];
    if (!route) throw new Error(`未知路由 ${id}`);
    return link(urlOf(route, l, params));
  };
  return {
    preview: PREVIEW, lang, L: C.ui, C, L2: CONTENT[lang === 'zh' ? 'en' : 'zh'].ui, cfg, data, tokens: TOKENS, page, root, link, href,
    nav: NAV, footer: FOOTER, a: (n) => root + 'assets/' + (names[n] || n), assetName: (n) => names[n] || n,
    fmtDate: (iso) => data.localDay(Date.parse(iso)),
    // 面包屑项：{ label, href（相对）, url（站点绝对路径，供 JSON-LD） }
    crumb: (id, label, params = {}) => ({ label, href: href(id, params), url: urlOf(ROUTE_BY_ID[id], lang, params) }),
  };
}
// 先用占位名渲染一遍页面，收集字符，再做字体子集
const draft = PAGES.map((p) => p.render(makeCtx(p, assetMap))).join('');

// ---------- 5. 字体 ----------
const FONT_SRC = {
  pixel: join(HERE, 'fonts/src/fusion-pixel-12px-proportional-zh_hans.otf.woff2'),
  noto: join(REPO_ROOT, 'internal/web/ui/fonts/NotoSansSC-gb2312.woff2'),
  geist: join(REPO_ROOT, 'internal/web/ui/fonts/Geist-latin.woff2'),
  geistmono: join(REPO_ROOT, 'internal/web/ui/fonts/GeistMono-latin.woff2'),
};
const ascii = Array.from({ length: 95 }, (_, i) => String.fromCharCode(32 + i)).join('');
const textOf = (o) => (typeof o === 'string' ? o : Array.isArray(o) ? o.map(textOf).join('') : o && typeof o === 'object' ? Object.values(o).map(textOf).join('') : '');
const pageText = draft.replace(/<script[\s\S]*?<\/script>/g, '').replace(/<[^>]*>/g, '');
// 像素字体用于标题、按钮、版本号等：取文案和版本号；正文字体取所有页面文字
const pixelChars = new Set(ascii + textOf(CONTENT) + data.releases.map((r) => r.tag).join('') + '0123456789');
const notoChars = new Set(ascii + pageText + textOf(CONTENT) + '，。、：；！？「」（）—…·→↗✓');
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
mkdirSync(join(OUT, 'assets/licenses'), { recursive: true });
for (const f of ['OFL-Geist.txt', 'OFL-NotoSansSC.txt']) copyFileSync(join(REPO_ROOT, 'internal/web/ui/fonts', f), join(OUT, 'assets/licenses', f));
for (const f of readdirSync(join(HERE, 'fonts/licenses'))) copyFileSync(join(HERE, 'fonts/licenses', f), join(OUT, 'assets/licenses', f));
// 许可证目录页（OSS 子目录首页）
writeFileSync(join(OUT, 'assets/licenses/index.html'), `<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>字体许可 · Font licenses · MineDeck</title></head><body><h1>字体许可 · Font licenses</h1><ul>${readdirSync(join(OUT, 'assets/licenses')).filter((f) => f.endsWith('.txt')).sort().map((f) => `<li><a href="${f}">${f}</a></li>`).join('')}</ul></body></html>\n`);

// ---------- 6. CSS / JS ----------
function tokensCss(t) {
  const out = [];
  const val = (v) => String(v).replace(/\{palette\.([\w-]+)\}/g, (m, k) => {
    if (!(k in t.palette)) throw new Error(`tokens.json: 未知色板 ${k}`);
    return `var(--palette-${k})`;
  });
  for (const [group, vals] of Object.entries(t)) {
    if (group.startsWith('_') || group === 'breakpoint') continue;
    if (Array.isArray(vals)) vals.forEach((v, i) => out.push(`--${group}-${i}:${v}`));
    else for (const [k, v] of Object.entries(vals)) out.push(`--${group}-${k}:${val(v)}`);
  }
  return `@layer tokens{:root{${out.join(';')}}}\n`;
}
// 层顺序即优先级；每个文件包进自己的 @layer。组件和页面样式里不允许颜色、硬阴影字面量。
const LAYERS = ['base', 'components', 'pages', 'motion'];
let css = `@layer tokens,${LAYERS.join(',')};\n` + tokensCss(TOKENS);
for (const layer of LAYERS) {
  const src = readText(join(HERE, 'src/css', `${layer}.css`));
  const bad = src.replace(/\/\*[\s\S]*?\*\//g, '').match(/#[0-9a-fA-F]{3,8}\b|rgba?\(|\d+px \d+px 0 /g);
  if (bad) { console.error(`src/css/${layer}.css 里有颜色或阴影字面量 ${bad.join(' ')}，请改用 tokens.json`); process.exit(1); }
  const depth = src.replace(/\/\*[\s\S]*?\*\//g, '').split('').reduce((d, c) => (d < 0 ? d : c === '{' ? d + 1 : c === '}' ? d - 1 : d), 0);
  if (depth !== 0) { console.error(`src/css/${layer}.css 花括号不配平`); process.exit(1); }
  css += `@layer ${layer}{\n${src}\n}\n`;
}
css = css.replace(/\{\{font:(\w+)\}\}/g, (m, k) => assetMap[`${k}.woff2`]);
emit('style.css', Buffer.from(css));
// 浏览器脚本：core 全站；其余按路由的 scripts 加载。feedback 由几个小模块按顺序拼成一个文件。
const JS = { 'core.js': ['core.js'], 'releases.js': ['releases.js'], 'feedback.js': ['feedback/redact.js', 'feedback/report.js', 'feedback/channels.js', 'feedback/form.js'] };
for (const [name, parts] of Object.entries(JS)) emit(name, Buffer.from(parts.map((p) => readText(join(HERE, 'src/js', p))).join('\n')));

// ---------- 7. 页面与数据 ----------
for (const p of PAGES) {
  const html = p.render(makeCtx(p, assetMap));
  mkdirSync(dirname(join(OUT, p.out)), { recursive: true });
  writeFileSync(join(OUT, p.out), html);
}
const site = cfg.siteUrl.replace(/\/$/, '');
const json = (f, o) => writeFileSync(join(OUT, 'data', f), JSON.stringify(o));
json('releases.json', {
  generated_at: data.generated_at, repo: data.repo, timezone: data.timezone, mirror_placeholder: data.mirror_placeholder,
  latest_stable: pickRelease(data.latest_stable), latest_beta: pickRelease(data.latest_beta),
  releases: data.releases.map((r) => ({ tag: r.tag, name: r.name, channel: r.channel, published_at: r.published_at, summary: r.summary, url: `${site}/releases/${encodeURIComponent(r.tag)}/`, assets: r.assets.map(({ name, size, sha256, mirror_url, github_url }) => ({ name, size, sha256, mirror_url, github_url })) })),
});
const latestOf = (r) => r && { tag: r.tag, date: r.published_at, url: `${site}/releases/${encodeURIComponent(r.tag)}/`, sha256: r.exe?.sha256 ?? null, size: r.exe?.size ?? null, download: r.exe?.mirror_url || r.exe?.github_url || null, github: r.exe?.github_url ?? null };
json('latest.json', { generated_at: data.generated_at, stable: latestOf(data.latest_stable), beta: latestOf(data.latest_beta) });
json('stats.json', { generated_at: data.generated_at, repo: data.repoInfo, contributors: data.contributors.list.map(({ login, contributions, html_url }) => ({ login, contributions, html_url })), activity: { source: data.activity.source, days: data.activity.days } });
// Atom
const xe = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
const feedItems = data.releases.slice(0, cfg.feedCount || 20);
writeFileSync(join(OUT, 'feed.xml'), `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xml:lang="zh-CN">
<title>MineDeck 更新日志</title>
<id>${site}/releases/</id>
<link rel="self" href="${site}/feed.xml"/>
<link href="${site}/releases/"/>
<updated>${feedItems[0]?.published_at ?? data.generated_at}</updated>
<author><name>MineDeck</name></author>
${feedItems.map((r) => `<entry><title>${xe(r.tag)}${r.prerelease ? '（测试版）' : ''}</title><id>${site}/releases/${encodeURIComponent(r.tag)}/</id><link href="${site}/releases/${encodeURIComponent(r.tag)}/"/><updated>${r.published_at}</updated><summary>${xe(r.summary)}</summary><content type="html">${xe(r.body_html.replace(/href="md-release:([^"]+)"/g, (m, t) => `href="${site}/releases/${encodeURIComponent(t)}/"`))}</content></entry>`).join('\n')}
</feed>
`);
writeFileSync(join(OUT, 'robots.txt'), `User-agent: *\nAllow: /\nSitemap: ${site}/sitemap.xml\n`);
writeFileSync(join(OUT, 'sitemap.xml'), `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">\n` +
  PAGES.filter((p) => !p.noSitemap).map((p) => `<url><loc>${site}${p.url}</loc><xhtml:link rel="alternate" hreflang="zh-CN" href="${site}${p.urls.zh}"/><xhtml:link rel="alternate" hreflang="en" href="${site}${p.urls.en}"/></url>`).join('\n') + '\n</urlset>\n');

// ---------- 8. 检查：运行时不得请求 GitHub 或国外 CDN ----------
const problems = checkDist(OUT);
if (problems.length) { console.error('构建检查未通过：\n' + problems.map((p) => '  ' + p).join('\n')); process.exit(1); }

if (!(cfg.feedback.channels || []).some((c) => c.type === 'endpoint' && c.enabled && c.url)) log('提醒：反馈页的「直接提交」通道还没开（site.config.json 的 feedback.channels 里 type 为 endpoint 的那一项）。没有 GitHub 账号的人目前只能复制或下载报告。');
log(`完成：${OUT}（${PAGES.length} 个页面）${cfg.mirror?.enabled ? '；镜像链接为占位模板 ' + cfg.mirror.template : ''}`);
for (const [k, v] of Object.entries(assetMap)) if (k.endsWith('.woff2')) log(`  ${v}  ${(statSync(join(OUT, 'assets', v)).size / 1024).toFixed(0)} KB`);
