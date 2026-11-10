import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { entry, releaseCalendar } from '../partials/release.mjs';

export default function changelog(ctx) {
  const { L, data, cfg } = ctx;
  const st = data.latest_stable, bt = data.latest_beta, last = data.releases[0];
  const cur = data.releases.filter((r) => !r.legacy), leg = data.releases.filter((r) => r.legacy);
  const group = (title, list, note) => list.length ? `<section data-group>
  <h2 class="group-h">${esc(title)}</h2>${note ? `<p class="group-note">${esc(note)}</p>` : ''}
  <div class="log">${list.map((r) => entry(ctx, r, { full: true })).join('\n')}</div></section>` : '';
  const main = `<section class="sec page-top">
 <div class="sec-h"><h1 class="pix page-title">${esc(L.log.h)}</h1><p>${esc(L.log.sub)}${L.log.notesLang ? ' ' + esc(L.log.notesLang) : ''}</p></div>
 <div class="stats">
  ${st ? `<a class="card stat s1" href="#${esc(st.tag)}"><small>${esc(L.log.latestStable)}</small><b>${esc(st.tag)}</b></a>` : ''}
  ${bt ? `<a class="card stat s2" href="#${esc(bt.tag)}"><small>${esc(L.log.latestBeta)}</small><b>${esc(bt.tag)}</b></a>` : ''}
  <div class="card stat s3"><small>${esc(L.log.total)}</small><b>${data.releases.length}</b></div>
  ${last ? `<div class="card stat s4"><small>${esc(L.log.lastRelease)}</small><b data-ago="${esc(last.published_at)}" data-tpl="${esc(L.log.daysAgo)}" data-today="${esc(L.log.today)}">${esc(last.date_local.slice(0, 10))}</b></div>` : ''}
 </div>
 ${releaseCalendar(ctx)}
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
</section>`;
  return layout(ctx, { title: L.changelogTitle, desc: L.changelogDescription, main });
}
