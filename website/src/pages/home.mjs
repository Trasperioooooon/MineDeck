import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { featCard, secHead } from '../partials/ui.mjs';
import { entry } from '../partials/release.mjs';

export default function home(ctx) {
  const { L, data, cfg } = ctx;
  const st = data.latest_stable, bt = data.latest_beta;
  const showBeta = bt && (!st || Date.parse(bt.published_at) > Date.parse(st.published_at));
  const exe = st && st.exe;
  const primary = exe ? (exe.mirror_url
    ? `<a class="btn primary" href="${esc(exe.mirror_url)}"><span class="pix">${esc(fmt(L.hero.download, { version: st.tag }))}</span><small>${esc(fmt(L.hero.mirrorSub, { file: exe.name, size: exe.size_label }))}</small></a>`
    : `<a class="btn primary" href="${esc(exe.github_url)}"><span class="pix">${esc(fmt(L.hero.download, { version: st.tag }))}</span><small>${esc(exe.name)} · ${esc(exe.size_label)}</small></a>`) : '';
  const secondary = exe ? `<a class="btn" href="${esc(exe.github_url)}"><span>${esc(L.hero.githubBtn)}</span><small>${esc(L.hero.githubSub)}</small></a>` : '';
  const cl = ctx.href('changelog');
  const main = `<section class="hero">
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
 ${secHead({ h: L.features.h, p: esc(fmt(L.features.sub, { stable: st ? st.tag : '' })) })}
 <div class="grid">${L.features.items.map((it, i) => featCard(it, i)).join('\n')}</div>
 <div class="band ai">
  ${secHead({ h: L.ai.h, p: esc(L.ai.sub) })}
  <div class="grid">${L.ai.items.map((it, i) => featCard(it, i + 7)).join('\n')}</div>
 </div>
 ${showBeta ? `<div class="band beta">
  ${secHead({ h: L.beta.h, p: esc(fmt(L.beta.sub, { introduced: L.beta.introduced })) })}
  <div class="grid">${L.beta.items.map((it, i) => featCard(it, i + 3, L.beta.badge)).join('\n')}</div>
 </div>` : ''}
</section>

<section class="sec" id="screenshots">
 ${secHead({ h: L.shots.h, p: esc(L.shots.sub) })}
 <div class="gallery">${L.shots.items.map((s, i) => `<figure class="card${i === 6 ? ' wide' : ''}"><a href="${ctx.a(s.f)}"><img src="${ctx.a(s.f)}" alt="${esc(s.alt)}" width="1440" height="900" loading="lazy"></a><figcaption>${esc(s.c)}</figcaption></figure>`).join('\n')}</div>
</section>

<section class="sec" id="changelog">
 ${secHead({ h: L.log.h, p: esc(L.log.homeSub) })}
 <div class="log">${data.releases.slice(0, 3).map((r) => entry(ctx, r, { full: false })).join('\n')}</div>
 <div class="more"><a class="btn" href="${cl}">${esc(L.log.viewAll)}</a></div>
</section>

<section class="sec">
 <div class="card oss"><div><h2>${esc(L.oss.h)}</h2><p>${esc(L.oss.p)}</p></div>
 <div class="row"><a class="btn primary" href="https://github.com/${cfg.repo}"><span>${esc(L.oss.star)}</span></a><a class="btn" href="https://github.com/${cfg.repo}/issues/new/choose"><span>${esc(L.oss.issue)}</span></a></div></div>
</section>`;
  return layout(ctx, { title: L.title, desc: L.description, main });
}
