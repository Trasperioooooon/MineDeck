// 页面模板。中英文共用同一套模板，文案来自 src/i18n/{zh,en}.json。
export const esc = (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
export const fmt = (s, v = {}) => String(s).replace(/\{(\w+)\}/g, (m, k) => (k in v ? v[k] : m));

const PALETTE_SIZE = 12; // tokens.json 的 palette 长度

function head(ctx, title, desc) {
  const { L, cfg, page, a } = ctx;
  const abs = (p) => cfg.siteUrl.replace(/\/$/, '') + p;
  const other = page.alt; // 另一种语言的同一页
  return `<!DOCTYPE html>
<html lang="${L.lang}" data-lang="${page.lang}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>${esc(title)}</title>
<meta name="description" content="${esc(desc)}">
<link rel="canonical" href="${abs(page.url)}">
${page.noAlt ? '' : `<link rel="alternate" hreflang="zh-CN" href="${abs(page.zhUrl)}"${page.lang === 'en' ? ` data-local="${ctx.link(page.zhUrl)}"` : ''}>
<link rel="alternate" hreflang="en" href="${abs(page.enUrl)}"${page.lang === 'zh' ? ` data-local="${ctx.link(page.enUrl)}"` : ''}>
<link rel="alternate" hreflang="x-default" href="${abs(page.zhUrl)}">`}
<meta name="theme-color" content="#f4ecd9">
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

function topbar(ctx, current) {
  const { L, page } = ctx;
  const home = page.lang === 'zh' ? '/' : '/en/';
  const cl = page.lang === 'zh' ? '/changelog/' : '/en/changelog/';
  const onHome = current === 'home';
  const otherUrl = page.lang === 'zh' ? page.enUrl : page.zhUrl;
  return `<div class="wrap"><header class="top">
 <a class="brand" href="${ctx.link(home)}"><img src="${ctx.a('logo-144.png')}" alt="" width="40" height="40"><span>MineDeck</span></a>
 <nav class="nav" aria-label="MineDeck">
  <a class="opt" href="${onHome ? '#features' : ctx.link(home) + '#features'}">${esc(L.nav.features)}</a>
  <a href="${ctx.link(cl)}"${current === 'changelog' ? ' aria-current="page"' : ''}>${esc(L.nav.changelog)}</a>
  <a class="opt" href="https://github.com/${ctx.cfg.repo}">${esc(L.nav.github)}</a>
  ${page.noAlt ? '' : `<span class="lang" role="group" aria-label="${esc(L.langSwitchAria)}">${page.lang === 'zh'
    ? `<span aria-current="true">中</span><a href="${ctx.link(otherUrl)}" hreflang="en" lang="en" data-set-lang="en">EN</a>`
    : `<a href="${ctx.link(otherUrl)}" hreflang="zh-CN" lang="zh-CN" data-set-lang="zh">中</a><span aria-current="true">EN</span>`}</span>`}
 </nav>
</header></div>`;
}

function footer(ctx) {
  const { L, cfg } = ctx;
  const icp = cfg.beian.icp ? `<a href="https://beian.miit.gov.cn/" rel="noopener">${esc(cfg.beian.icp)}</a>` : esc(L.footer.pending);
  const psb = cfg.beian.psb
    ? `<a href="https://beian.mps.gov.cn/#/query/webSearch?code=${esc(cfg.beian.psbCode)}" rel="noopener">${esc(cfg.beian.psb)}</a>`
    : esc(L.footer.pending);
  return `<div class="dirt" aria-hidden="true"></div>
<footer><div class="wrap">
 <div class="row"><span>${esc(L.footer.copyright)}</span><span>${fmt(esc(L.footer.icp), { v: icp })}</span><span>${fmt(esc(L.footer.psb), { v: psb })}</span>
 <a href="https://github.com/${cfg.repo}">GitHub</a><a href="https://github.com/${cfg.repo}/issues">Issues</a></div>
 <p>${esc(L.footer.disclaimer)}</p>
 <p>${esc(L.footer.fonts)}</p>
</div></footer>
</body></html>
`;
}

function featCard(it, i, badge) {
  return `<div class="card feat"><div class="ico" style="--c:var(--palette-${i % PALETTE_SIZE})" aria-hidden="true"></div><h3>${esc(it.t)}${badge ? ` <span class="badge bt">${esc(badge)}</span>` : ''}</h3><p>${esc(it.d)}</p></div>`;
}

function badge(L, r) {
  return r.prerelease ? `<span class="badge bt">${esc(L.log.betaF)}</span>` : `<span class="badge st">${esc(L.log.stable)}</span>`;
}

function dlButtons(ctx, r, small) {
  const { L } = ctx;
  const exe = r.exe;
  if (!exe) return `<a class="btn sm" href="${esc(r.html_url)}">${esc(L.log.onGithub)}</a>`;
  const cls = small ? 'btn sm' : 'btn';
  return (exe.mirror_url ? `<a class="${cls} primary" href="${esc(exe.mirror_url)}">${esc(L.log.mirror)} · ${esc(exe.name)}</a>` : '') +
    `<a class="${cls}" href="${esc(exe.github_url)}">${esc(L.log.github)} · ${esc(exe.size_label)}</a>`;
}

function entry(ctx, r, { full }) {
  const { L } = ctx;
  const others = r.assets.filter((x) => x !== r.exe);
  const zhAttr = ctx.page.lang === 'en' ? ' lang="zh-CN"' : '';
  const lead = `<p class="summary"${r.summary ? zhAttr : ''}>${esc(r.summary || L.log.noNotes)}</p>`;
  const notes = r.body_html
    ? lead + `<div class="notes"${ctx.page.lang === 'en' ? ' lang="zh-CN"' : ''}>${r.body_html}</div><button type="button" class="toggle" aria-expanded="false" data-more="${esc(L.log.expand)}" data-less="${esc(L.log.collapse)}">${esc(L.log.expand)}</button>`
    : lead;
  const body = full ? notes : `<p class="summary"${ctx.page.lang === 'en' && r.summary ? ' lang="zh-CN"' : ''}>${esc(r.summary || L.log.noNotes)}</p>`;
  return `<article class="entry" id="${esc(r.tag)}" data-type="${r.prerelease ? 'beta' : 'stable'}">
 <div class="av" aria-hidden="true"><img src="${ctx.a('logo-144.png')}" alt="" width="34" height="34"></div>
 <div class="card">
  <div class="meta"><a class="ver" href="${full ? '#' + esc(r.tag) : ctx.link(ctx.page.lang === 'zh' ? '/changelog/' : '/en/changelog/') + '#' + esc(r.tag)}">${esc(r.tag)}</a>${badge(L, r)}<time datetime="${esc(r.published_at)}">${esc(r.date_local)} UTC+${ctx.cfg.timezoneOffsetHours}</time>${r.exe ? `<span>${esc(r.exe.size_label)}</span>` : ''}</div>
  ${body}
  ${full ? `<div class="dl">${dlButtons(ctx, r, true)}<a class="gh" href="${esc(r.html_url)}">${esc(L.log.onGithub)} →</a></div>
  ${others.length ? `<details class="files"><summary>${esc(L.log.otherFiles)} (${others.length})</summary><ul>${others.map((x) => `<li><a href="${esc(x.github_url)}">${esc(x.name)}</a> · ${esc(x.size_label)}${x.mirror_url ? ` · <a href="${esc(x.mirror_url)}">${esc(L.log.mirror)}</a>` : ''}</li>`).join('')}</ul></details>` : ''}
  ${r.exe && r.exe.sha256 ? `<div class="sha">SHA-256 ${esc(r.exe.name)}: ${esc(r.exe.sha256)}</div>` : ''}` : ''}
 </div>
</article>`;
}

export function homePage(ctx) {
  const { L, data, cfg } = ctx;
  const st = data.latest_stable, bt = data.latest_beta;
  const showBeta = bt && (!st || Date.parse(bt.published_at) > Date.parse(st.published_at));
  const exe = st && st.exe;
  const primary = exe ? (exe.mirror_url
    ? `<a class="btn primary" href="${esc(exe.mirror_url)}"><span class="pix">${esc(fmt(L.hero.download, { version: st.tag }))}</span><small>${esc(fmt(L.hero.mirrorSub, { file: exe.name, size: exe.size_label }))}</small></a>`
    : `<a class="btn primary" href="${esc(exe.github_url)}"><span class="pix">${esc(fmt(L.hero.download, { version: st.tag }))}</span><small>${esc(exe.name)} · ${esc(exe.size_label)}</small></a>`) : '';
  const secondary = exe ? `<a class="btn" href="${esc(exe.github_url)}"><span>${esc(L.hero.githubBtn)}</span><small>${esc(L.hero.githubSub)}</small></a>` : '';
  const recent = data.releases.slice(0, 3);
  const cl = ctx.link(ctx.page.lang === 'zh' ? '/changelog/' : '/en/changelog/');
  return head(ctx, L.title, L.description) + topbar(ctx, 'home') + `
<main id="main" class="wrap">
<section class="hero">
 <div>
  <span class="eyebrow">${esc(L.hero.eyebrow)}</span>
  <h1>${L.hero.h1}</h1>
  <p class="lead">${esc(L.hero.lead)}</p>
  <div class="cta">${primary}${secondary}</div>
  <div class="sub-links">${showBeta ? `<a href="${cl}#${esc(bt.tag)}">${esc(fmt(L.hero.betaLink, { version: bt.tag }))}</a>` : ''}<a href="https://github.com/${cfg.repo}/releases">${esc(L.hero.allReleases)}</a><a href="${cl}">${esc(L.nav.changelog)}</a></div>
  <p class="note">${esc(L.hero.note)}</p>
  ${exe && exe.sha256 ? `<div class="sha">${esc(L.hero.sha)} ${esc(exe.name)}: ${esc(exe.sha256)}</div>` : ''}
 </div>
 <div class="card shot"><div class="grass-strip" aria-hidden="true"></div><img src="${ctx.a('01-servers.png')}" alt="${esc(L.hero.shotAlt)}" width="1440" height="900"></div>
</section>

<section class="sec" id="features">
 <div class="sec-h"><h2>${esc(L.features.h)}</h2><p>${esc(fmt(L.features.sub, { stable: st ? st.tag : '' }))}</p></div>
 <div class="grid">${L.features.items.map((it, i) => featCard(it, i)).join('\n')}</div>
 <div class="band ai">
  <div class="sec-h"><h2>${esc(L.ai.h)}</h2><p>${esc(L.ai.sub)}</p></div>
  <div class="grid">${L.ai.items.map((it, i) => featCard(it, i + 7)).join('\n')}</div>
 </div>
 ${showBeta ? `<div class="band beta">
  <div class="sec-h"><h2>${esc(L.beta.h)}</h2><p>${esc(fmt(L.beta.sub, { introduced: L.beta.introduced }))}</p></div>
  <div class="grid">${L.beta.items.map((it, i) => featCard(it, i + 3, L.beta.badge)).join('\n')}</div>
 </div>` : ''}
</section>

<section class="sec" id="screenshots">
 <div class="sec-h"><h2>${esc(L.shots.h)}</h2><p>${esc(L.shots.sub)}</p></div>
 <div class="gallery">${L.shots.items.map((s, i) => `<figure class="card${i === 6 ? ' wide' : ''}"><a href="${ctx.a(s.f)}"><img src="${ctx.a(s.f)}" alt="${esc(s.alt)}" width="1440" height="900" loading="lazy"></a><figcaption>${esc(s.c)}</figcaption></figure>`).join('\n')}</div>
</section>

<section class="sec" id="changelog">
 <div class="sec-h"><h2>${esc(L.log.h)}</h2><p>${esc(L.log.homeSub)}</p></div>
 <div class="log">${recent.map((r) => entry(ctx, r, { full: false })).join('\n')}</div>
 <div class="more"><a class="btn" href="${cl}">${esc(L.log.viewAll)}</a></div>
</section>

<section class="sec">
 <div class="card oss"><div><h2>${esc(L.oss.h)}</h2><p>${esc(L.oss.p)}</p></div>
 <div class="row"><a class="btn primary" href="https://github.com/${cfg.repo}"><span>${esc(L.oss.star)}</span></a><a class="btn" href="https://github.com/${cfg.repo}/issues/new/choose"><span>${esc(L.oss.issue)}</span></a></div></div>
</section>
</main>
` + footer(ctx);
}

function calendar(ctx) {
  const { data, L, cfg } = ctx;
  const off = cfg.timezoneOffsetHours * 3600e3;
  const day = (t) => new Date(t + off).toISOString().slice(0, 10);
  const map = {};
  for (const r of data.releases) {
    const k = day(Date.parse(r.published_at));
    (map[k] ||= []).push(r);
  }
  const buildT = Date.parse(data.generated_at);
  const first = data.releases.length ? Math.min(...data.releases.map((r) => Date.parse(r.published_at))) : buildT;
  let start = Math.min(first - 14 * 86400e3, buildT - 26 * 7 * 86400e3);
  start = Math.max(start, buildT - 52 * 7 * 86400e3);
  // 对齐到周日（本地时区）
  let d = new Date(day(start) + 'T00:00:00Z');
  d = new Date(d.getTime() - d.getUTCDay() * 86400e3);
  const end = new Date(day(buildT) + 'T00:00:00Z');
  const endWeek = new Date(end.getTime() + (6 - end.getUTCDay()) * 86400e3);
  const cells = [];
  const from = d.toISOString().slice(0, 10);
  for (; d <= endWeek; d = new Date(d.getTime() + 86400e3)) {
    const k = d.toISOString().slice(0, 10);
    const rs = map[k];
    if (d > end) cells.push('<span class="x"></span>');
    else if (rs) {
      const cls = rs.some((r) => !r.prerelease) ? 's' : 'b';
      const t = `${k}：${rs.map((r) => r.tag).join('、')}`;
      cells.push(`<a class="${cls}" href="#${esc(rs[0].tag)}" title="${esc(t)}" aria-label="${esc(t)}"></a>`);
    } else cells.push(`<span title="${k}"></span>`);
  }
  return `<div class="card cal"><div class="cal-scroll"><div class="cal-grid" role="img" aria-label="${esc(L.log.calendar)}">${cells.join('')}</div></div>
 <div class="legend"><span><i style="background:var(--color-grass)"></i>${esc(L.log.stable)}</span><span><i style="background:var(--color-beta)"></i>${esc(L.log.betaF)}</span><span>${esc(fmt(L.log.calendarRange, { from, to: end.toISOString().slice(0, 10) }))}</span></div></div>`;
}

export function changelogPage(ctx) {
  const { L, data, cfg } = ctx;
  const st = data.latest_stable, bt = data.latest_beta, last = data.releases[0];
  const cur = data.releases.filter((r) => !r.legacy), leg = data.releases.filter((r) => r.legacy);
  const group = (title, list, note) => list.length ? `<section data-group>
  <h2 class="group-h">${esc(title)}</h2>${note ? `<p class="group-note">${esc(note)}</p>` : ''}
  <div class="log">${list.map((r) => entry(ctx, r, { full: true })).join('\n')}</div></section>` : '';
  return head(ctx, L.changelogTitle, L.changelogDescription) + topbar(ctx, 'changelog') + `
<main id="main" class="wrap">
<section class="sec" style="padding-top:12px">
 <div class="sec-h"><h1 class="pix" style="font-size:48px;line-height:1.2;margin:0">${esc(L.log.h)}</h1><p>${esc(L.log.sub)}${L.log.notesLang ? ' ' + esc(L.log.notesLang) : ''}</p></div>
 <div class="stats">
  ${st ? `<a class="card stat s1" href="#${esc(st.tag)}"><small>${esc(L.log.latestStable)}</small><b>${esc(st.tag)}</b></a>` : ''}
  ${bt ? `<a class="card stat s2" href="#${esc(bt.tag)}"><small>${esc(L.log.latestBeta)}</small><b>${esc(bt.tag)}</b></a>` : ''}
  <div class="card stat s3"><small>${esc(L.log.total)}</small><b>${data.releases.length}</b></div>
  ${last ? `<div class="card stat s4"><small>${esc(L.log.lastRelease)}</small><b data-ago="${esc(last.published_at)}" data-tpl="${esc(L.log.daysAgo)}" data-today="${esc(L.log.today)}">${esc(last.date_local.slice(0, 10))}</b></div>` : ''}
 </div>
 ${calendar(ctx)}
 <div class="filters" role="group" aria-label="${esc(L.log.filterAria)}">
  <button type="button" class="chip" data-type="all" aria-pressed="true">${esc(L.log.all)}</button>
  <button type="button" class="chip" data-type="stable" aria-pressed="false">${esc(L.log.stable)}</button>
  <button type="button" class="chip" data-type="beta" aria-pressed="false">${esc(L.log.betaF)}</button>
 </div>
 <div id="releases" data-initial="${cfg.changelogInitial}">
 ${group(L.log.groupCurrent, cur)}
 ${group(L.log.groupLegacy, leg, L.log.legacyNote)}
 </div>
 <div class="more"><button type="button" class="btn is-hidden" id="show-all" data-tpl="${esc(L.log.showAll)}">${esc(fmt(L.log.showAll, { n: data.releases.length }))}</button></div>
</section>
</main>
` + footer(ctx);
}

export function notFoundPage(ctx) {
  const { L } = ctx;
  const Z = ctx.L2; // 另一种语言
  return head(ctx, L.notFound.title + ' · MineDeck', L.description) + topbar(ctx, '') + `
<main id="main" class="wrap nf">
 <h1>404</h1>
 <p>${esc(L.notFound.p)} <span lang="en">${esc(Z.notFound.p)}</span></p>
 <p><a class="btn" href="/">${esc(L.notFound.back)}</a> <a class="btn" href="/en/">${esc(Z.notFound.back)}</a></p>
</main>
` + footer(ctx);
}
