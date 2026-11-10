// 页面骨架：<head>、顶栏、<main>、页脚。每个页面只提供标题、描述和主体。
import { esc } from './util.mjs';
import { nav } from './nav.mjs';
import { footer } from './footer.mjs';

function head(ctx, title, desc) {
  const { L, cfg, page, a } = ctx;
  const abs = (p) => cfg.siteUrl.replace(/\/$/, '') + p;
  const alt = (lang, hreflang) => `<link rel="alternate" hreflang="${hreflang}" href="${abs(page.urls[lang])}"${page.lang !== lang ? ` data-local="${ctx.link(page.urls[lang])}"` : ''}>`;
  return `<!DOCTYPE html>
<html lang="${L.lang}" data-lang="${page.lang}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>${esc(title)}</title>
<meta name="description" content="${esc(desc)}">
<link rel="canonical" href="${abs(page.url)}">
${page.noAlt ? '' : `${alt('zh', 'zh-CN')}
${alt('en', 'en')}
<link rel="alternate" hreflang="x-default" href="${abs(page.urls.zh)}">`}
<meta name="theme-color" content="${ctx.tokens.color.bg}">
<link rel="icon" href="${ctx.root}favicon.ico" sizes="any">
<link rel="icon" type="image/png" href="${a('logo-144.png')}">
<link rel="apple-touch-icon" href="${a('logo-144.png')}">
${ctx.preview ? '' : `<link rel="preload" href="${a('pixel.woff2')}" as="font" type="font/woff2" crossorigin>`}
<link rel="stylesheet" href="${a('style.css')}">
<meta property="og:type" content="website">
<meta property="og:site_name" content="MineDeck">
<meta property="og:title" content="${esc(title)}">
<meta property="og:description" content="${esc(desc)}">
<meta property="og:url" content="${abs(page.url)}">
<meta property="og:image" content="${abs('/assets/' + ctx.assetName('social-preview.png'))}">
<meta property="og:locale" content="${page.lang === 'zh' ? 'zh_CN' : 'en_US'}">
<meta name="twitter:card" content="summary_large_image">
<script src="${a('app.js')}" defer></script>
</head>
<body>
<a class="skip" href="#main">${esc(L.skip)}</a>`;
}

/**
 * @param ctx 构建上下文（见 build.mjs makeCtx）
 * @param {{title:string, desc:string, main:string, mainClass?:string}} o
 */
export function layout(ctx, { title, desc, main, mainClass = '' }) {
  return head(ctx, title, desc) + nav(ctx) + `
<main id="main" class="wrap${mainClass ? ' ' + mainClass : ''}">
${main}
</main>
` + footer(ctx);
}
