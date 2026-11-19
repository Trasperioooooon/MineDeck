#!/usr/bin/env node
// 发版前文档核对脚本：梳理仍标 beta:true、since 晚于最新稳定版、引用旧版截图及缺少英文译文的文档。
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseFrontMatter } from '../lib/markdown.mjs';

const HERE = resolve(fileURLToPath(new URL('.', import.meta.url)), '..');
const DOCS_DIR = join(HERE, 'docs-content');
const RELEASES_PATH = join(HERE, 'data/releases.cache.json');

function parseSemver(tag) {
  const m = String(tag || '').trim().replace(/^v/, '').match(/^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$/);
  if (!m) return null;
  return { major: +m[1], minor: +m[2], patch: +m[3], pre: m[4] || '' };
}

function cmpSemver(a, b) {
  const pa = parseSemver(a), pb = parseSemver(b);
  if (!pa || !pb) return 0;
  for (const k of ['major', 'minor', 'patch']) {
    if (pa[k] !== pb[k]) return pa[k] - pb[k];
  }
  if (!pa.pre && !pb.pre) return 0;
  if (!pa.pre) return 1;
  if (!pb.pre) return -1;
  return pa.pre.localeCompare(pb.pre, 'en', { numeric: true });
}

function walkMarkdown(lang) {
  const root = join(DOCS_DIR, lang);
  const out = [];
  if (!existsSync(root)) return out;
  for (const cat of readdirSync(root).sort()) {
    if (cat.startsWith('_') || cat.startsWith('.')) continue;
    const catDir = join(root, cat);
    if (!statSync(catDir).isDirectory()) continue;
    for (const f of readdirSync(catDir).sort()) {
      if (!f.endsWith('.md') || f.startsWith('_')) continue;
      const full = join(catDir, f);
      if (!statSync(full).isFile()) continue;
      const raw = readFileSync(full, 'utf8');
      const fm = parseFrontMatter(raw);
      out.push({
        lang,
        cat,
        slug: f.slice(0, -3),
        rel: relative(HERE, full).replace(/\\/g, '/'),
        fm: fm.data,
        body: fm.body,
      });
    }
  }
  return out;
}

const asJson = process.argv.includes('--json');
const rawReleases = existsSync(RELEASES_PATH) ? JSON.parse(readFileSync(RELEASES_PATH, 'utf8')) : [];
const releases = rawReleases
  .filter((r) => !r.draft && r.published_at)
  .sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at));
const stable = releases.find((r) => !r.prerelease);
const beta = releases.find((r) => r.prerelease);
const stableTag = stable ? (stable.tag_name || stable.tag || '') : '';
const betaTag = beta ? (beta.tag_name || beta.tag || '') : '';

const zhDocs = walkMarkdown('zh').filter((d) => d.fm.draft !== true);
const enSet = new Set(walkMarkdown('en').filter((d) => d.fm.draft !== true).map((d) => `${d.cat}/${d.slug}`));

const betaFlagged = [];
const newerThanStable = [];
const legacyShots = [];
const missingEn = [];

for (const d of zhDocs) {
  const key = `${d.cat}/${d.slug}`;
  if (d.fm.beta === true) betaFlagged.push({ file: d.rel, title: d.fm.title, since: d.fm.since || '' });
  if (d.fm.since && stableTag && cmpSemver(String(d.fm.since), stableTag) > 0) {
    newerThanStable.push({ file: d.rel, title: d.fm.title, since: d.fm.since });
  }
  const shotMatches = [...d.body.matchAll(/docs\/images\/(0[1-7]-[a-z-]+\.png)/g)].map((m) => m[1]);
  if (shotMatches.length) {
    legacyShots.push({ file: d.rel, title: d.fm.title, images: [...new Set(shotMatches)] });
  }
  if (!enSet.has(key)) {
    missingEn.push({ file: d.rel, expectedEn: `docs-content/en/${key}.md` });
  }
}

const report = {
  latestStable: stableTag || null,
  latestBeta: betaTag || null,
  zhPublishedCount: zhDocs.length,
  enPublishedCount: enSet.size,
  betaFlagged,
  newerThanStable,
  legacyShots,
  missingEn,
};

if (asJson) {
  console.log(JSON.stringify(report, null, 2));
} else {
  console.log(`=== MineDeck 文档发版核对报告 ===`);
  console.log(`当前缓存稳定版: ${stableTag || '无'} | 最新测试版: ${betaTag || '无'}`);
  console.log(`已发布文档数量: 中文 ${zhDocs.length} 篇 | 英文 ${enSet.size} 篇\n`);

  const printGroup = (heading, items, fmt) => {
    console.log(`[${heading}] (${items.length})`);
    if (!items.length) console.log('  (无)');
    else items.forEach((it) => console.log('  - ' + fmt(it)));
    console.log('');
  };

  printGroup('1. 仍标记 beta:true 的文章', betaFlagged, (x) => `${x.file} (${x.title}${x.since ? `, since ${x.since}` : ''})`);
  printGroup(`2. since 晚于当前稳定版 ${stableTag || ''} 的文章`, newerThanStable, (x) => `${x.file} (since=${x.since})`);
  printGroup('3. 引用了 v2.9.0-beta.1 旧版截图的文章', legacyShots, (x) => `${x.file} -> ${x.images.join(', ')}`);
  printGroup('4. 尚未提供英文译文（当前走自动回退页）的文章', missingEn, (x) => `${x.file} -> ${x.expectedEn}`);
}
