// 文档中心构建期校验。失败给出文件路径和行号，错误正文不带回命中的原文。
// 缺英文译文只警告；已发布的英文稿没有已发布中文稿则失败。

const RESERVED_PATHS = new Set([
  '/', '/download/', '/features/', '/releases/', '/help/', '/feedback/', '/about/', '/changelog/', '/docs/',
]);

// 匹配前会把反斜杠折成斜杠并转成小写。只拦用户目录和家目录，不拦文档里举例用的安装目录（例如 D:\Games\MineDeck）。
const LEAK_RULES = [
  { name: '本机绝对路径', re: /(?:[a-z]:\/users\/|[a-z]:\/users\b|\/users\/[a-z0-9._-]+|\/home\/[a-z0-9._-]+)/ },
  { name: '疑似密钥或令牌', re: /\b(?:sk-[a-z0-9]{16,}|ghp_[a-z0-9]{20,}|github_pat_[a-z0-9_]{20,}|aiza[a-z0-9_-]{20,})\b|\bbearer\s+[a-z0-9._-]{16,}/ },
  { name: '内部禁公开目录或文件', re: /(?:docs\/(?:日志|测试与评审|调研|归档|设计与参考)(?:\/|$|[^a-z0-9_])|docs\/(?:待办|现状)\.md|\.(?:claude|review|mimocode)(?:\/))/ },
];

const FILEISH = /\.(?:html?|md|json|xml|txt|png|jpe?g|gif|webp|svg|js|css|ico|pdf)$/i;

export function normalizeAlias(raw, langPrefix = '') {
  let u = String(raw || '').trim();
  if (!u.startsWith('/')) u = '/' + u;
  if (!u.endsWith('/')) u += '/';
  if (langPrefix && !u.startsWith(langPrefix + '/')) u = langPrefix + u;
  return u;
}

/** 别名必须是站内目录。缺尾部斜杠可以，规范化时会补上。版本号里的点（v3.2.2）允许。 */
export function aliasShapeError(raw) {
  const u = String(raw ?? '').trim();
  if (!u.startsWith('/')) return 'alias 必须以 / 开头';
  if (/^[a-z][a-z0-9+.-]*:/i.test(u)) return 'alias 不能带协议';
  if (u.includes('//')) return 'alias 不能包含 //';
  if (/[?#\\\s]/.test(u)) return 'alias 不能包含查询串、锚点、反斜杠或空白';
  const segs = u.split('/');
  for (const seg of segs) {
    if (seg === '.' || seg === '..') return 'alias 不能包含 . 或 ..';
  }
  const last = u.replace(/\/+$/, '').split('/').pop() || '';
  if (FILEISH.test(last)) return 'alias 不能指向文件';
  return '';
}

/** 和 build.mjs 的输出文件名对齐，并按大小写折叠，避免 Windows 上覆盖。 */
export function outputKey(url) {
  let u = String(url || '').trim();
  if (!u.startsWith('/')) u = '/' + u;
  const file = (u.endsWith('/') ? u + 'index.html' : u).replace(/^\//, '');
  return file.toLowerCase();
}

export function sinceError(value) {
  if (value == null || value === '') return '';
  const s = String(value);
  if (!/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.]+)?$/.test(s)) {
    return 'since 须为 v主.次.修，可带预发布号，例如 v3.2.2 或 v3.2.0-beta.3';
  }
  return '';
}

export function canonSitePath(p) {
  let u = String(p || '');
  if (!u.startsWith('/')) u = '/' + u;
  if (u.length > 1 && !u.endsWith('/') && !/\.[a-z0-9]+$/i.test(u)) u += '/';
  return u;
}

/** 英文页上没写 /en 的站内路径，渲染时会加上 /en，校验按加上之后的地址查。 */
export function effectiveSitePath(lang, path) {
  let p = canonSitePath(path);
  if (lang === 'en' && p !== '/en/' && !p.startsWith('/en/') && !p.startsWith('/assets/')) {
    p = p === '/' ? '/en/' : '/en' + p;
  }
  return p;
}

/**
 * 拆开文档里的链接，供校验和渲染共用。
 * query 含前导 ?，fragment 是 # 后面的原文，anchor 是解码后的锚点。
 */
export function parseDocHref(raw) {
  const u = String(raw || '').trim();
  if (!u) return { kind: 'empty' };
  if (/^mailto:/i.test(u)) return { kind: 'mailto', url: u };
  if (/^https?:\/\//i.test(u)) return { kind: 'external', url: u };
  if (/^[a-z][a-z0-9+.-]*:/i.test(u)) return { kind: 'unsupported', url: u };
  let path = u;
  let query = '';
  let fragment = '';
  const hashAt = path.indexOf('#');
  if (hashAt >= 0) {
    fragment = path.slice(hashAt + 1);
    path = path.slice(0, hashAt);
  }
  const qAt = path.indexOf('?');
  if (qAt >= 0) {
    query = path.slice(qAt);
    path = path.slice(0, qAt);
  }
  let anchor = fragment;
  if (anchor) {
    try { anchor = decodeURIComponent(anchor); } catch { /* 保留原文 */ }
  }
  if (!path) return { kind: 'hash', anchor, query, fragment };
  if (/\.md$/i.test(path) || (!path.startsWith('/') && !/^[a-z][a-z0-9+.-]*:/i.test(path))) {
    return { kind: 'md', path, query, anchor, fragment };
  }
  if (path.startsWith('/')) return { kind: 'site', path, query, anchor, fragment };
  return { kind: 'unsupported', url: u };
}

function unprefixedAlias(al) {
  if (al === '/en' || al === '/en/') return '/';
  if (al.startsWith('/en/')) return al.slice(3) || '/';
  return al;
}

function isReservedAlias(al) {
  let p = unprefixedAlias(al);
  if (!p.endsWith('/') && !/\.[a-z0-9]+$/i.test(p)) p += '/';
  if (!p.startsWith('/')) p = '/' + p;
  return RESERVED_PATHS.has(p);
}

/**
 * 校验全部解析后的文档。
 * sitePaths：正式页面、两种语言的文档（含英文回退页）和别名。
 * aliasTargets：规范化后的别名 URL → 目标文档。
 * @returns {{ errors: string[], warnings: string[] }}
 */
export function validateDocs({ docs, metaCategories, resolveRelTarget, resolveImgPath, sitePaths, aliasTargets }) {
  const errors = [];
  const warnings = [];
  const catIds = new Set(metaCategories.map((c) => c.id));
  const seenOrder = new Map();
  const seenSlug = new Map();
  const seenAlias = new Map();
  const byPathKey = new Map();
  const pages = sitePaths || new Set();
  const aliases = aliasTargets || new Map();

  for (const d of docs) byPathKey.set(`${d.lang}/${d.cat}/${d.slug}`, d);

  const lookupDoc = (path) => {
    const m = String(path).match(/^\/(?:en\/)?docs\/([^/]+)\/([^/]+)\/$/);
    if (m) {
      const lang = path.startsWith('/en/') ? 'en' : 'zh';
      const own = byPathKey.get(`${lang}/${m[1]}/${m[2]}`);
      if (own && !own.draft) return { doc: own, kind: 'doc' };
      if (lang === 'en') {
        const zh = byPathKey.get(`zh/${m[1]}/${m[2]}`);
        if (zh && !zh.draft) return { doc: zh, kind: 'fallback' };
      }
    }
    if (aliases.has(path)) return { doc: aliases.get(path), kind: 'alias' };
    return null;
  };

  for (const d of docs) {
    const f = d.fileRel;
    const fm = d.fm;
    const lineOf = (k) => d.fmLines[k] || 1;

    for (const e of d.fmErrors) errors.push(`${f}:${e.line}: ${e.message}`);

    if (!catIds.has(d.cat)) {
      errors.push(`${f}:1: 未知文档分类 "${d.cat}"，请先在 docs-content/_meta.json 注册`);
    }

    if (typeof fm.title !== 'string' || !fm.title.trim()) {
      errors.push(`${f}:${lineOf('title')}: front-matter 缺少必填字段 title（非空字符串）`);
    }
    if (typeof fm.description !== 'string' || !fm.description.trim()) {
      errors.push(`${f}:${lineOf('description')}: front-matter 缺少必填字段 description（非空字符串）`);
    }
    if (typeof fm.order !== 'number' || !Number.isFinite(fm.order)) {
      errors.push(`${f}:${lineOf('order')}: front-matter 缺少必填数字字段 order`);
    }
    for (const boolKey of ['beta', 'featured', 'draft']) {
      if (boolKey in fm && typeof fm[boolKey] !== 'boolean') {
        errors.push(`${f}:${lineOf(boolKey)}: front-matter 字段 ${boolKey} 必须是布尔值 true/false`);
      }
    }
    if ('tags' in fm && !Array.isArray(fm.tags)) {
      errors.push(`${f}:${lineOf('tags')}: front-matter 字段 tags 必须是字符串数组`);
    }
    if ('aliases' in fm && !Array.isArray(fm.aliases)) {
      errors.push(`${f}:${lineOf('aliases')}: front-matter 字段 aliases 必须是字符串数组`);
    }
    if ('updated' in fm && fm.updated !== '' && !/^\d{4}-\d{2}-\d{2}$/.test(String(fm.updated))) {
      errors.push(`${f}:${lineOf('updated')}: front-matter 字段 updated 须为 YYYY-MM-DD 格式`);
    }
    if ('since' in fm) {
      const why = sinceError(fm.since);
      if (why) errors.push(`${f}:${lineOf('since')}: ${why}`);
    }

    const slugKey = `${d.lang}:${d.cat}:${d.slug}`;
    if (seenSlug.has(slugKey)) {
      errors.push(`${f}:1: 与 ${seenSlug.get(slugKey)} 存在重复路径 ${d.cat}/${d.slug}`);
    } else {
      seenSlug.set(slugKey, f);
    }

    if (!d.draft && typeof fm.order === 'number') {
      const ordKey = `${d.lang}:${d.cat}:${fm.order}`;
      if (seenOrder.has(ordKey)) {
        errors.push(`${f}:${lineOf('order')}: order=${fm.order} 与同分类下 ${seenOrder.get(ordKey)} 冲突`);
      } else {
        seenOrder.set(ordKey, f);
      }
    }

    if (!d.draft && Array.isArray(fm.aliases)) {
      const langPrefix = d.lang === 'zh' ? '' : `/${d.lang}`;
      for (const rawAl of fm.aliases) {
        const shape = aliasShapeError(rawAl);
        if (shape) {
          errors.push(`${f}:${lineOf('aliases')}: ${shape}（${rawAl}）`);
          continue;
        }
        const al = normalizeAlias(rawAl, langPrefix);
        const alKey = al.toLowerCase();
        if (isReservedAlias(al)) {
          errors.push(`${f}:${lineOf('aliases')}: alias "${al}" 与站点内置路由冲突`);
        } else if (seenAlias.has(alKey)) {
          errors.push(`${f}:${lineOf('aliases')}: alias "${al}" 与 ${seenAlias.get(alKey)} 重复`);
        } else {
          seenAlias.set(alKey, f);
        }
        const targetMatch = al.match(/^\/(?:en\/)?docs\/([^/]+)\/([^/]+)\/$/);
        if (targetMatch) {
          const langOfAl = al.startsWith('/en/') ? 'en' : 'zh';
          const key = `${langOfAl}/${targetMatch[1]}/${targetMatch[2]}`;
          const hit = byPathKey.get(key);
          if (hit && !hit.draft) {
            errors.push(`${f}:${lineOf('aliases')}: alias "${al}" 与现存正式文档冲突`);
          } else if (langOfAl === 'en') {
            const zh = byPathKey.get(`zh/${targetMatch[1]}/${targetMatch[2]}`);
            if (zh && !zh.draft) {
              errors.push(`${f}:${lineOf('aliases')}: alias "${al}" 与英文回退页冲突`);
            }
          }
        }
      }
    }

    const allLines = d.rawSrc.replace(/\r\n?/g, '\n').split('\n');
    allLines.forEach((txt, idx) => {
      const norm = txt.replace(/\\/g, '/').toLowerCase();
      for (const rule of LEAK_RULES) {
        if (rule.re.test(norm)) {
          errors.push(`${f}:${idx + 1}: 检测到${rule.name}，请移除后再构建`);
        }
      }
    });

    if (d.parsed.h1Warnings.length) {
      for (const ln of d.parsed.h1Warnings) {
        warnings.push(`${f}:${ln}: 正文建议使用 ## 二级小标题（# 一级标题由文章 title 自动生成，当前已自动降为 h2）`);
      }
    }

    if (d.draft) continue;

    for (const img of d.parsed.images) {
      const src = String(img.src || '').trim();
      if (/^https?:\/\//i.test(src) || src.startsWith('//')) {
        errors.push(`${f}:${img.line}: 文档图片不能使用远程地址：${src}`);
        continue;
      }
      if (/^[a-z][a-z0-9+.-]*:/i.test(src)) {
        errors.push(`${f}:${img.line}: 不支持的图片地址：${src}`);
        continue;
      }
      const abs = resolveImgPath(d, src);
      if (!abs) {
        errors.push(`${f}:${img.line}: 引用的图片不存在或超出允许目录：${src}`);
      }
    }

    for (const ref of d.parsed.refs) {
      const u = ref.url;
      const parsed = parseDocHref(u);
      if (parsed.kind === 'external' || parsed.kind === 'mailto') continue;
      if (parsed.kind === 'hash') {
        if (parsed.anchor && !d.parsed.headingIds.has(parsed.anchor)) {
          errors.push(`${f}:${ref.line}: 页内锚点 #${parsed.anchor} 在当前文章不存在`);
        }
        continue;
      }
      if (parsed.kind === 'md') {
        const res = resolveRelTarget(d, u);
        if (!res) {
          errors.push(`${f}:${ref.line}: 相对文档链接无法解析：${u}`);
        } else if (res.error) {
          errors.push(`${f}:${ref.line}: ${res.error}（${u}）`);
        } else if (res.anchor && !res.target.parsed.headingIds.has(res.anchor)) {
          errors.push(`${f}:${ref.line}: 目标文档 ${res.target.fileRel} 中不存在锚点 #${res.anchor}`);
        }
        continue;
      }
      if (parsed.kind === 'site') {
        const path = effectiveSitePath(d.lang, parsed.path);
        if (!pages.has(path)) {
          errors.push(`${f}:${ref.line}: 站内链接指向不存在的页面：${u}`);
          continue;
        }
        if (parsed.anchor) {
          const hit = lookupDoc(path);
          if (hit && hit.kind === 'fallback') {
            errors.push(`${f}:${ref.line}: 英文回退页没有正文，锚点 #${parsed.anchor} 不存在`);
          } else if (hit && !hit.doc.parsed.headingIds.has(parsed.anchor)) {
            errors.push(`${f}:${ref.line}: 目标文档 ${hit.doc.fileRel} 中不存在锚点 #${parsed.anchor}`);
          }
        }
        continue;
      }
      if (parsed.kind === 'unsupported' || parsed.kind === 'empty') {
        errors.push(`${f}:${ref.line}: 不支持的链接：${u}`);
      }
    }
  }

  const zhPublished = docs.filter((d) => d.lang === 'zh' && !d.draft);
  for (const z of zhPublished) {
    const en = byPathKey.get(`en/${z.cat}/${z.slug}`);
    if (!en || en.draft) {
      warnings.push(`缺少英文译文 website/docs-content/en/${z.cat}/${z.slug}.md，将生成英文回退页`);
    }
  }

  for (const d of docs) {
    if (d.lang !== 'en' || d.draft) continue;
    const zh = byPathKey.get(`zh/${d.cat}/${d.slug}`);
    const expect = `website/docs-content/zh/${d.cat}/${d.slug}.md`;
    if (!zh) {
      errors.push(`${d.fileRel}:1: 已发布的英文稿没有对应中文稿 ${expect}`);
    } else if (zh.draft) {
      errors.push(`${d.fileRel}:1: 对应中文稿仍是草稿 ${expect}，已发布英文稿不能单独上线`);
    }
  }

  return { errors, warnings };
}
