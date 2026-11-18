// 功能（ARCH 4.3）：按功能分组，左侧粘性目录。每组先一句大白话（lead）和几条要点（points），
// 技术细节（details）和界面截图收在 <details> 里，点开才看。测试版的条目和整组带徽标。
// 内容在 content/features.*.json，事实以仓库的 docs/功能清单.md 为准，改那边时这里要跟着改。
import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { pageHead, toc, go } from '../partials/ui.mjs';
import { icon } from '../partials/svg.mjs';

// 哪组配哪几张截图（docs/images）
const SHOTS = { cores: ['01-servers.png'], diag: ['02-console-banner.png'], ai: ['03-assistant.png', '04-gameai-log.png', '05-companion.png', '07-settings-ai.png'], music: ['06-music.png'] };

export default function features(ctx) {
  const { L, C, data } = ctx;
  const F = C.features;
  const badge = `<span class="badge bt">${esc(L.log.betaF)}</span>`;
  const shotOf = (f) => L.shots.items.find((s) => s.f === f);
  const li = (it) => (typeof it === 'string' ? `<li>${esc(it)}</li>` : `<li class="is-beta">${badge} ${esc(it.t)}</li>`);
  const groups = F.groups.map((g, gi) => {
    const shots = (SHOTS[g.id] || []).map(shotOf).filter(Boolean);
    const more = (g.details && g.details.length) || shots.length
      ? `<details class="fmore">
   <summary>${esc(F.moreDetails)}</summary>
   ${g.details && g.details.length ? `<ul class="flist">${g.details.map(li).join('')}</ul>` : ''}
   ${shots.length ? `<p class="fshots-h">${esc(F.shotsLabel)}</p><div class="fshots">${shots.map((s) => `<figure><a href="${ctx.a(s.f)}"><img src="${ctx.a(s.f)}" alt="${esc(s.alt)}" width="1440" height="900" loading="lazy"></a><figcaption>${esc(s.c)}</figcaption></figure>`).join('')}</div>` : ''}
  </details>`
      : '';
    return `<section class="fgroup card" id="${esc(g.id)}">
  <h2 class="fgroup-h"><span class="ico" style="--c:var(--icon-${gi % 12})">${icon(g.icon || 'block', { cls: 'feat-ico' })}</span><span class="pix">${esc(g.h)}</span>${g.channel === 'beta' ? ' ' + badge : ''}</h2>
  <p class="fgroup-lead">${esc(g.lead)}</p>
  <ul class="flist">${g.points.map(li).join('')}</ul>
  ${more}
 </section>`;
  }).join('\n');
  const lead = esc(fmt(F.lead, { stable: data.latest_stable ? data.latest_stable.tag : '', beta: data.latest_beta ? data.latest_beta.tag : '' }));
  const main = `${pageHead(ctx, { h: F.title, lead, crumbs: [ctx.crumb('home', L.crumbHome), ctx.crumb('features', F.title)] })}
<div class="with-toc">
 <aside class="toc-col">${toc(F.toc, F.groups.map((g) => ({ id: g.id, text: g.h })))}</aside>
 <div class="toc-main">
 ${groups}
 <div class="card more-box"><b>${esc(F.more.h)}</b>${go(ctx.href('help'), F.more.help)}${go(ctx.href('download'), F.more.download)}</div>
 <p class="note">${esc(L.shots.sub)}</p>
 </div>
</div>`;
  return layout(ctx, { title: L.features.title, desc: F.desc, main });
}
