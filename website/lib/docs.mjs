// 文档中心模型加载与渲染：扫描 docs-content/，校验前置规则，生成分类树、上下篇、别名跳转、git 更新时间与静态搜索索引。
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { dirname, extname, join, posix, relative, resolve } from 'node:path';
import { parseFrontMatter, parseDoc, renderParsedDoc } from './markdown.mjs';
import { validateDocs, normalizeAlias, aliasShapeError, parseDocHref, canonSitePath, effectiveSitePath } from './doc-checks.mjs';
import { ROUTES, LANGS } from '../src/routes.mjs';
import { icon } from '../src/partials/svg.mjs';
import { extLink } from '../src/partials/ui.mjs';

const IMG_EXT = new Set(['.png', '.jpg', '.jpeg', '.webp', '.gif', '.svg']);

function readImageSize(buf, ext) {
  try {
    if (ext === '.png' && buf.length >= 24 && buf.toString('ascii', 1, 4) === 'PNG') {
      return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) };
    }
    if (ext === '.gif' && buf.length >= 10) {
      return { width: buf.readUInt16LE(6), height: buf.readUInt16LE(8) };
    }
    if ((ext === '.jpg' || ext === '.jpeg') && buf.length > 4) {
      let i = 2;
      while (i + 9 < buf.length) {
        if (buf[i] !== 0xff) break;
        const marker = buf[i + 1];
        if (marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc) {
          return { height: buf.readUInt16BE(i + 5), width: buf.readUInt16BE(i + 7) };
        }
        const len = buf.readUInt16BE(i + 2);
        if (len < 2) break;
        i += 2 + len;
      }
    }
  } catch {}
  return {};
}

function walkDocs(dir, lang) {
  const out = [];
  if (!existsSync(dir)) return out;
  for (const cat of readdirSync(dir).sort()) {
    if (cat.startsWith('_') || cat.startsWith('.')) continue;
    const catPath = join(dir, cat);
    if (!statSync(catPath).isDirectory()) continue;
    for (const file of readdirSync(catPath).sort()) {
      if (!file.endsWith('.md') || file.startsWith('_') || file.startsWith('.')) continue;
      const fullPath = join(catPath, file);
      if (!statSync(fullPath).isFile()) continue;
      const slug = file.slice(0, -3);
      out.push({ lang, cat, slug, fullPath });
    }
  }
  return out;
}

function gitUpdatedDay(repoRoot, fileRelFromRepo, tzHours, fallback) {
  try {
    const r = spawnSync('git', ['-C', repoRoot, 'log', '-1', '--format=%aI', '--', fileRelFromRepo], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });
    if (r.status === 0 && r.stdout.trim()) {
      const t = Date.parse(r.stdout.trim());
      if (!Number.isNaN(t)) return new Date(t + tzHours * 3600e3).toISOString().slice(0, 10);
    }
  } catch {}
  return fallback || null;
}

function buildSearchPayload(treeDocs) {
  return {
    v: 1,
    docs: treeDocs.map((d) => ({
      id: `${d.cat}/${d.slug}`,
      u: `${d.cat}/${d.slug}/`,
      t: d.title,
      c: d.catTitle,
      d: d.description,
      k: Array.isArray(d.tags) ? d.tags : [],
      b: !!d.beta,
      s: d.since || '',
      l: d.untranslated ? 'zh' : '',
      p: d.sections.map((s) => ({
        a: s.a,
        h: s.h,
        x: s.x.length > 600 ? s.x.slice(0, 600) : s.x,
      })),
    })),
  };
}

function collectSitePaths(here, docs, aliasIndex) {
  const paths = new Set();
  const add = (u) => {
    if (!u) return;
    paths.add(canonSitePath(u));
  };
  let releaseTags = [];
  try {
    const raw = JSON.parse(readFileSync(join(here, 'data/releases.cache.json'), 'utf8'));
    if (Array.isArray(raw)) {
      releaseTags = raw.filter((r) => r && !r.draft && (r.tag_name || r.tag)).map((r) => r.tag_name || r.tag);
    }
  } catch { /* 没有缓存时版本页不进清单，构建前面会先拦住 */ }
  for (const route of ROUTES) {
    if (route.id === 'doc') continue;
    const langs = route.langs || Object.keys(LANGS);
    const paramSets = route.id === 'release' ? releaseTags.map((tag) => ({ tag })) : [{}];
    for (const lang of langs) {
      for (const params of paramSets) {
        const path = route.path.replace(/\{(\w+)\}/g, (m, k) => encodeURIComponent(params[k] ?? ''));
        add((LANGS[lang] || '') + path);
      }
    }
  }
  for (const d of docs) {
    if (d.draft) continue;
    const prefix = d.lang === 'zh' ? '' : '/en';
    add(`${prefix}/docs/${d.cat}/${d.slug}/`);
    if (d.lang === 'zh') add(`/en/docs/${d.cat}/${d.slug}/`);
  }
  for (const url of aliasIndex.keys()) add(url);
  return paths;
}

export function loadDocsModel({ here, repoRoot, emit, tzHours = 8, log = () => {} }) {
  const docsDir = join(here, 'docs-content');
  const metaPath = join(docsDir, '_meta.json');
  const meta = JSON.parse(readFileSync(metaPath, 'utf8'));
  const categories = [...meta.categories].sort((a, b) => a.order - b.order);
  const catById = Object.fromEntries(categories.map((c) => [c.id, c]));

  const allowedImgRoots = [resolve(repoRoot, 'docs/images'), resolve(docsDir)];
  const isInside = (target, root) => {
    const rel = relative(root, target);
    return rel === '' || (!rel.startsWith('..') && !resolve(root, rel).includes('\0'));
  };

  const docs = [];
  for (const lang of ['zh', 'en']) {
    for (const item of walkDocs(join(docsDir, lang), lang)) {
      const rawSrc = readFileSync(item.fullPath, 'utf8');
      const fileRel = posix.normalize(relative(here, item.fullPath).replace(/\\/g, '/'));
      const repoRel = relative(repoRoot, item.fullPath).replace(/\\/g, '/');
      const fmRes = parseFrontMatter(rawSrc);
      const parsed = parseDoc(fmRes.body, { bodyStartLine: fmRes.bodyStartLine });
      const fm = fmRes.data;
      const draft = fm.draft === true;
      docs.push({
        lang,
        cat: item.cat,
        slug: item.slug,
        fullPath: item.fullPath,
        fileRel,
        repoRel,
        rawSrc,
        fm,
        fmLines: fmRes.fmLines,
        fmErrors: fmRes.errors,
        draft,
        title: String(fm.title || item.slug),
        description: String(fm.description || ''),
        order: typeof fm.order === 'number' ? fm.order : 9999,
        since: fm.since ? String(fm.since) : '',
        beta: fm.beta === true,
        featured: fm.featured === true,
        tags: Array.isArray(fm.tags) ? fm.tags.map(String) : [],
        updated: gitUpdatedDay(repoRoot, repoRel, tzHours, fm.updated ? String(fm.updated) : ''),
        aliases: Array.isArray(fm.aliases) ? fm.aliases.map(String) : [],
        parsed,
        sections: parsed.sections,
      });
    }
  }

  const byKey = new Map(docs.map((d) => [`${d.lang}/${d.cat}/${d.slug}`, d]));

  // 别名索引要在校验之前建好：相对 .md 和功能页关联都靠它找到改名后的文章。
  const aliasIndex = new Map();
  for (const d of docs) {
    if (d.draft) continue;
    const prefix = d.lang === 'zh' ? '' : `/${d.lang}`;
    for (const raw of d.aliases) {
      if (aliasShapeError(raw)) continue;
      const from = normalizeAlias(raw, prefix);
      if (!aliasIndex.has(from)) aliasIndex.set(from, d);
    }
  }

  const resolveRelTarget = (fromDoc, rawUrl) => {
    const parsed = parseDocHref(rawUrl);
    if (!parsed || parsed.kind !== 'md') return null;
    const cleanPath = parsed.path;
    if (!cleanPath || !cleanPath.endsWith('.md')) return null;
    const targetAbs = resolve(dirname(fromDoc.fullPath), cleanPath);
    const langRoot = resolve(docsDir, fromDoc.lang);
    if (!isInside(targetAbs, langRoot)) {
      return { error: `相对 .md 链接不能跨出 docs-content/${fromDoc.lang}` };
    }
    const rel = relative(langRoot, targetAbs).replace(/\\/g, '/');
    const parts = rel.split('/');
    if (parts.length !== 2 || !parts[1].endsWith('.md')) {
      return { error: `相对文档路径格式无效：${cleanPath}` };
    }
    const slug = parts[1].slice(0, -3);
    let targetDoc = byKey.get(`${fromDoc.lang}/${parts[0]}/${slug}`);
    if (!targetDoc || targetDoc.draft) {
      const prefix = fromDoc.lang === 'zh' ? '' : `/${fromDoc.lang}`;
      const via = aliasIndex.get(`${prefix}/docs/${parts[0]}/${slug}/`)
        || (fromDoc.lang === 'en' ? aliasIndex.get(`/docs/${parts[0]}/${slug}/`) : null);
      if (via && !via.draft) targetDoc = via;
    }
    if (!targetDoc) return { error: `目标文档不存在：${cleanPath}` };
    if (targetDoc.draft) return { error: `目标文档是草稿（draft: true），正式页面不能链接` };
    return { target: targetDoc, anchor: parsed.anchor || '', query: parsed.query || '', fragment: parsed.fragment || '' };
  };

  const resolveImgPath = (fromDoc, rawSrc) => {
    const clean = rawSrc.split('?')[0].split('#')[0];
    const ext = extname(clean).toLowerCase();
    if (!IMG_EXT.has(ext)) return null;
    let abs = resolve(dirname(fromDoc.fullPath), clean);
    if (!existsSync(abs)) {
      const fromRepo = resolve(repoRoot, clean.replace(/^\/+/, ''));
      if (existsSync(fromRepo)) abs = fromRepo;
    }
    if (!existsSync(abs) || !statSync(abs).isFile()) return null;
    if (!allowedImgRoots.some((r) => isInside(abs, r))) return null;
    return abs;
  };

  const sitePaths = collectSitePaths(here, docs, aliasIndex);
  const checkRes = validateDocs({
    docs, metaCategories: categories, resolveRelTarget, resolveImgPath, sitePaths, aliasTargets: aliasIndex,
  });
  for (const w of checkRes.warnings) log(`警告：${w}`);
  if (checkRes.errors.length) {
    throw new Error('文档校验未通过：\n' + checkRes.errors.map((e) => '  ' + e).join('\n'));
  }

  // 注册并输出文档引用的全部本地图片资源
  const imgMeta = new Map(); // absPath -> { assetKey, width, height }
  for (const d of docs) {
    if (d.draft) continue;
    for (const img of d.parsed.images) {
      if (/^https?:\/\//i.test(img.src)) continue;
      const abs = resolveImgPath(d, img.src);
      if (!abs || imgMeta.has(abs)) continue;
      const buf = readFileSync(abs);
      const ext = extname(abs).toLowerCase();
      const relDocs = relative(docsDir, abs).replace(/\\/g, '/');
      const logical = relDocs.startsWith('..')
        ? `docs/${relative(resolve(repoRoot, 'docs/images'), abs).replace(/\\/g, '/')}`
        : `docs/${relDocs}`;
      emit(logical, buf);
      const dims = readImageSize(buf, ext);
      imgMeta.set(abs, { logical, ...dims });
    }
  }

  // 按 zh 为基准构建完整语言视图，确保 en 缺失文章自动产出回退页并纳入路由
  const zhPublished = docs.filter((d) => d.lang === 'zh' && !d.draft);
  const enPublished = docs.filter((d) => d.lang === 'en' && !d.draft);
  const publishedMap = { zh: new Map(), en: new Map() };
  for (const d of zhPublished) publishedMap.zh.set(`${d.cat}/${d.slug}`, d);
  for (const d of enPublished) publishedMap.en.set(`${d.cat}/${d.slug}`, d);

  // 所有已发布文章参数列表（对称存在于 zh 与 en，保证 hreflang 一致）
  // 路由只跟已发布的中文稿走。没有中文稿的英文稿在校验阶段已经失败。
  const routeKeySet = new Set(publishedMap.zh.keys());
  const treeByLang = { zh: [], en: [] };
  const flatByLang = { zh: [], en: [] };
  const articleByKey = { zh: new Map(), en: new Map() };
  const aliases = [];

  for (const lang of ['zh', 'en']) {
    for (const c of categories) {
      const items = [];
      for (const key of routeKeySet) {
        const [cat, slug] = key.split('/');
        if (cat !== c.id) continue;
        const own = publishedMap[lang].get(key);
        const fallbackSrc = !own ? publishedMap.zh.get(key) : null;
        if (!own && !fallbackSrc) continue;
        const src = own || fallbackSrc;
        items.push({
          ...src,
          lang,
          sourceLang: own ? lang : 'zh',
          untranslated: !own,
          catTitle: (c[lang] || c.zh).title,
          url: `${lang === 'zh' ? '' : '/en'}/docs/${cat}/${slug}/`,
          zhUrl: `/docs/${cat}/${slug}/`,
        });
      }
      items.sort((a, b) => a.order - b.order || a.slug.localeCompare(b.slug));
      if (items.length) {
        treeByLang[lang].push({
          id: c.id,
          order: c.order,
          icon: c.icon,
          title: (c[lang] || c.zh).title,
          desc: (c[lang] || c.zh).desc,
          items,
        });
        for (const it of items) {
          flatByLang[lang].push(it);
          articleByKey[lang].set(`${it.cat}/${it.slug}`, it);
        }
      }
    }
    flatByLang[lang].forEach((it, idx) => {
      it.prev = idx > 0 ? flatByLang[lang][idx - 1] : null;
      it.next = idx + 1 < flatByLang[lang].length ? flatByLang[lang][idx + 1] : null;
    });
  }

  for (const d of [...zhPublished, ...enPublished]) {
    const prefix = d.lang === 'zh' ? '' : `/${d.lang}`;
    const targetUrl = `${prefix}/docs/${d.cat}/${d.slug}/`;
    for (const rawAl of d.aliases) {
      const fromUrl = normalizeAlias(rawAl, prefix);
      aliases.push({
        lang: d.lang, fromUrl, targetUrl, title: d.title,
        fileRel: d.fileRel, line: d.fmLines.aliases || 1, raw: rawAl,
      });
    }
  }

  const routeParams = flatByLang.zh.map((it) => ({ cat: it.cat, slug: it.slug }));
  const searchIndex = {
    zh: buildSearchPayload(flatByLang.zh),
    en: buildSearchPayload(flatByLang.en),
  };

  // 输出 file:// --preview 模式下的懒加载脚本包装，避免本地 file:// fetch 被浏览器 CORS 拦截
  for (const lang of ['zh', 'en']) {
    const jsPayload = `window.__MD_DOCS_IDX=window.__MD_DOCS_IDX||{};window.__MD_DOCS_IDX[${JSON.stringify(lang)}]=${JSON.stringify(searchIndex[lang])};`;
    emit(`docs-index.${lang}.js`, Buffer.from(jsPayload));
  }

  const extraPixelChars = [
    ...categories.map((c) => c.zh.title + c.en.title),
    ...flatByLang.zh.map((d) => d.title + d.since),
    ...flatByLang.en.map((d) => d.title + d.since),
  ].join('');

  const aliasFor = (lang, cat, slug) => {
    const prefixed = `${lang === 'zh' ? '' : '/en'}/docs/${cat}/${slug}/`;
    if (aliasIndex.has(prefixed)) return aliasIndex.get(prefixed);
    if (lang === 'en' && aliasIndex.has(`/docs/${cat}/${slug}/`)) return aliasIndex.get(`/docs/${cat}/${slug}/`);
    return null;
  };

  return {
    categories,
    catById,
    treeByLang,
    flatByLang,
    articleByKey,
    routeParams,
    aliases,
    searchIndex,
    extraPixelChars,
    hasDoc: (lang, cat, slug) => publishedMap[lang]?.has(`${cat}/${slug}`) || publishedMap.zh.has(`${cat}/${slug}`) || !!aliasFor(lang, cat, slug),
    aliasTarget: (lang, cat, slug) => {
      if (publishedMap[lang]?.has(`${cat}/${slug}`) || publishedMap.zh.has(`${cat}/${slug}`)) return null;
      const via = aliasFor(lang, cat, slug);
      return via ? { cat: via.cat, slug: via.slug } : null;
    },
    renderArticle: (doc, ctx) => {
      const { L } = ctx;
      const copyIcon = icon('copy', { cls: 'i-copy' });
      const doneIcon = icon('check', { cls: 'i-check' });
      const calloutIcons = {
        note: icon('block', { cls: '' }),
        tip: icon('check', { cls: '' }),
        warning: icon('alert', { cls: '' }),
        caution: icon('alert', { cls: '' }),
      };
      return renderParsedDoc(doc.parsed, {
        copyIcon,
        doneIcon,
        copyLabel: L.copy,
        copiedLabel: L.copied,
        extSr: L.extSr,
        renderExtLink: (url, html) => extLink(ctx, url, '', { html }),
        calloutTitles: L.docs.callouts,
        calloutIcons,
        resolveImage: (rawSrc) => {
          if (/^https?:\/\//i.test(rawSrc) || String(rawSrc || '').trim().startsWith('//')) return null;
          const abs = resolveImgPath(doc, rawSrc);
          const info = abs && imgMeta.get(abs);
          if (!info) return null;
          return { src: ctx.a(info.logical), width: info.width, height: info.height };
        },
        resolveLink: (rawUrl) => {
          const parsed = parseDocHref(rawUrl);
          if (parsed.kind === 'external') {
            return { url: parsed.url, ext: true };
          }
          if (parsed.kind === 'mailto') return { url: parsed.url, ext: false };
          if (parsed.kind === 'hash') return { url: rawUrl.startsWith('#') ? rawUrl : `#${parsed.fragment || ''}`, ext: false };
          const tail = (query, fragment) => (query || '') + (fragment ? `#${fragment}` : '');
          if (parsed.kind === 'md') {
            const res = resolveRelTarget(doc, rawUrl);
            if (!res || res.error) return null;
            const prefix = doc.lang === 'zh' ? '' : '/en';
            const base = ctx.link(`${prefix}/docs/${res.target.cat}/${res.target.slug}/`);
            return { url: base + tail(res.query, res.fragment), ext: false };
          }
          if (parsed.kind === 'site') {
            const path = effectiveSitePath(doc.lang, parsed.path);
            return { url: ctx.link(path) + tail(parsed.query, parsed.fragment), ext: false };
          }
          return null;
        },
      });
    },
  };
}
