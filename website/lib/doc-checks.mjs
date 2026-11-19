// 文档中心构建期校验：校验失败给出 exact 文件路径与行号；缺失英文译文仅作警告，不阻断构建。

const RESERVED_PATHS = new Set([
  '/', '/download/', '/features/', '/releases/', '/help/', '/feedback/', '/about/', '/changelog/', '/docs/',
]);

const LEAK_RULES = [
  { name: '本机绝对路径', re: /(?:[A-Za-z]:\\Users\\[^\s`'")]+|E:\\MineDeck\b|\/Users\/[A-Za-z0-9._-]+|\/home\/[A-Za-z0-9._-]+)/ },
  { name: '疑似密钥或令牌', re: /\b(?:sk-[A-Za-z0-9]{16,}|ghp_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AIza[A-Za-z0-9_-]{20,})\b|\bBearer\s+[A-Za-z0-9._-]{16,}/ },
  { name: '内部禁公开目录或文件', re: /(?:docs\/(?:日志|测试与评审|调研|归档|待办\.md|现状\.md|设计与参考\/)|\.(?:claude|review|mimocode)(?:\/|\\))/ },
];

export function normalizeAlias(raw, langPrefix = '') {
  let u = String(raw || '').trim();
  if (!u.startsWith('/')) u = '/' + u;
  if (!u.endsWith('/')) u += '/';
  if (langPrefix && !u.startsWith(langPrefix + '/')) u = langPrefix + u;
  return u;
}

/**
 * 校验全部解析后的文档。
 * @returns {{ errors: string[], warnings: string[] }}
 */
export function validateDocs({ docs, metaCategories, resolveRelTarget, resolveImgPath }) {
  const errors = [];
  const warnings = [];
  const catIds = new Set(metaCategories.map((c) => c.id));
  const seenOrder = new Map(); // `${lang}:${cat}:${order}` -> fileRel
  const seenSlug = new Map();  // `${lang}:${cat}:${slug}` -> fileRel
  const seenAlias = new Map(); // aliasUrl -> fileRel
  const byPathKey = new Map(); // `${lang}/${cat}/${slug}` -> doc

  for (const d of docs) byPathKey.set(`${d.lang}/${d.cat}/${d.slug}`, d);

  for (const d of docs) {
    const f = d.fileRel;
    const fm = d.fm;
    const lineOf = (k) => d.fmLines[k] || 1;

    // 1. 解析阶段的 front-matter 语法错误
    for (const e of d.fmErrors) errors.push(`${f}:${e.line}: ${e.message}`);

    // 2. 分类校验
    if (!catIds.has(d.cat)) {
      errors.push(`${f}:1: 未知文档分类 "${d.cat}"，请先在 docs-content/_meta.json 注册`);
    }

    // 3. 必填字段与类型
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

    // 4. 重复 slug / order / aliases 冲突
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
        const al = normalizeAlias(rawAl, langPrefix);
        if (RESERVED_PATHS.has(al)) {
          errors.push(`${f}:${lineOf('aliases')}: alias "${al}" 与站点内置路由冲突`);
        } else if (seenAlias.has(al)) {
          errors.push(`${f}:${lineOf('aliases')}: alias "${al}" 与 ${seenAlias.get(al)} 重复`);
        } else {
          seenAlias.set(al, f);
        }
      }
    }

    // 5. 正文安全与泄露扫描（含草稿也扫描，防止泄露进仓库）
    const allLines = d.rawSrc.replace(/\r\n?/g, '\n').split('\n');
    allLines.forEach((txt, idx) => {
      for (const rule of LEAK_RULES) {
        const gre = new RegExp(rule.re.source, 'g');
        for (const m of txt.matchAll(gre)) {
          errors.push(`${f}:${idx + 1}: 检测到${rule.name} "${m[0]}"，请移除后再构建`);
        }
      }
    });

    if (d.parsed.h1Warnings.length) {
      for (const ln of d.parsed.h1Warnings) {
        warnings.push(`${f}:${ln}: 正文建议使用 ## 二级小标题（# 一级标题由文章 title 自动生成，当前已自动降为 h2）`);
      }
    }

    if (d.draft) continue;

    // 6. 检查别名是否与已发布文档路径冲突
    if (Array.isArray(fm.aliases)) {
      const langPrefix = d.lang === 'zh' ? '' : `/${d.lang}`;
      for (const rawAl of fm.aliases) {
        const al = normalizeAlias(rawAl, langPrefix);
        const targetMatch = al.match(/^\/(?:en\/)?docs\/([^/]+)\/([^/]+)\/$/);
        if (targetMatch && byPathKey.has(`${d.lang}/${targetMatch[1]}/${targetMatch[2]}`) && !byPathKey.get(`${d.lang}/${targetMatch[1]}/${targetMatch[2]}`).draft) {
          errors.push(`${f}:${lineOf('aliases')}: alias "${al}" 与现存正式文档冲突`);
        }
      }
    }

    // 7. 图片存在性校验
    for (const img of d.parsed.images) {
      if (/^https?:\/\//i.test(img.src)) continue;
      const abs = resolveImgPath(d, img.src);
      if (!abs) {
        errors.push(`${f}:${img.line}: 引用的图片不存在或超出允许目录：${img.src}`);
      }
    }

    // 8. 站内链接与 .md 相对路径 / 锚点校验
    for (const ref of d.parsed.refs) {
      const u = ref.url;
      if (/^https?:\/\//i.test(u) || /^mailto:/i.test(u)) continue;
      if (u.startsWith('#')) {
        let anchor = u.slice(1);
        try { anchor = decodeURIComponent(anchor); } catch {}
        if (!d.parsed.headingIds.has(anchor)) {
          errors.push(`${f}:${ref.line}: 页内锚点 #${anchor} 在当前文章不存在`);
        }
        continue;
      }
      if (/\.md(?:$|[?#])/i.test(u) || (!u.startsWith('/') && !/^[a-z]+:/i.test(u))) {
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
      if (u.startsWith('/')) {
        const clean = u.split('#')[0].split('?')[0];
        const m = clean.match(/^\/(?:en\/)?docs\/([^/]+)\/([^/]+)\/?$/);
        if (m) {
          const targetDoc = byPathKey.get(`${d.lang}/${m[1]}/${m[2]}`);
          if (!targetDoc || targetDoc.draft) {
            errors.push(`${f}:${ref.line}: 站内文档链接指向不存在或草稿页面：${u}`);
          } else {
            const hash = u.includes('#') ? u.slice(u.indexOf('#') + 1) : '';
            if (hash) {
              let dec = hash;
              try { dec = decodeURIComponent(hash); } catch {}
              if (!targetDoc.parsed.headingIds.has(dec)) {
                errors.push(`${f}:${ref.line}: 站内文档 ${targetDoc.fileRel} 中不存在锚点 #${dec}`);
              }
            }
          }
        }
      }
    }
  }

  // 9. 中英文对照缺失检查（仅警告，不阻断构建；实际渲染生成英文回退页）
  const zhPublished = docs.filter((d) => d.lang === 'zh' && !d.draft);
  for (const z of zhPublished) {
    const en = byPathKey.get(`en/${z.cat}/${z.slug}`);
    if (!en || en.draft) {
      warnings.push(`缺少英文译文 website/docs-content/en/${z.cat}/${z.slug}.md，将生成英文回退页`);
    }
  }

  return { errors, warnings };
}
