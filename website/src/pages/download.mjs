// 下载（ARCH 4.2）：稳定版大卡、测试版、伙伴模组、安装三步（A6）、校验、系统要求、最近版本、镜像说明。
import { esc, fmt } from '../partials/util.mjs';
import { layout } from '../partials/layout.mjs';
import { pageHead, secHead, copyable, go, button, extLink, versionBadge } from '../partials/ui.mjs';
import { dlButtons, releaseMeta } from '../partials/release.mjs';
import { illustration } from '../partials/svg.mjs';

export default function download(ctx) {
  const { L, data, cfg } = ctx;
  const D = L.download;
  const st = data.latest_stable, bt = data.latest_beta;
  const relCard = (r, { title, cls, extra = '' }) => `<div class="card dl-card ${cls}">
  <div class="dl-card-h"><h2 class="pix">${esc(title)}</h2>${releaseMeta(ctx, r)}</div>
  ${extra}
  <div class="cta">${dlButtons(ctx, r)}</div>
  ${r.exe && r.exe.sha256 ? `<div class="sha-row"><small>SHA-256 · ${esc(r.exe.name)}</small>${copyable(ctx, r.exe.sha256, { label: L.release.copySha })}</div>` : ''}
  ${go(ctx.href('release', { tag: r.tag }), D.notesLink)}
 </div>`;
  const companions = st ? st.assets.filter((a) => a.kind.startsWith('companion')) : [];
  const smart = illustration('smartscreen', {
    label: D.smartAria, attrs: 'data-frames="3"',
    text: D.smart,
  });
  const hist = data.releases.slice(0, 5).map((r) => `<tr><td><a href="${ctx.href('release', { tag: r.tag })}">${esc(r.tag)}</a></td><td>${versionBadge(L, r)}</td><td>${esc(r.day_local)}</td><td>${r.exe ? esc(r.exe.size_label) : '—'}</td><td class="acts">${r.exe ? (r.exe.mirror_url ? button({ href: r.exe.mirror_url, label: D.mirrorBtn, size: 'sm', icon: 'arrow' }) : '') + extLink(ctx, r.exe.github_url, 'GitHub', { cls: 'gh-alt' }) : ''}</td></tr>`).join('');
  const main = `${pageHead(ctx, { h: D.h, lead: esc(D.lead), crumbs: [ctx.crumb('home', L.crumbHome), ctx.crumb('download', D.h)] })}
<section class="sec dl-grid">
 ${st ? relCard(st, { title: D.recommended, cls: 'is-stable' }) : ''}
 ${data.show_beta ? relCard(bt, { title: D.beta, cls: 'is-beta', extra: `<p>${esc(D.betaDiff)}</p><p class="notice">${esc(D.betaWarn)}</p>` }) : ''}
</section>

${companions.length ? `<section class="sec" id="companion">
 ${secHead({ h: D.companionH, p: esc(D.companionP) })}
 <div class="grid cols-2">${companions.map((f) => `<div class="card comp"><b>${esc(f.kind === 'companion-neoforge' ? D.companionNeo : D.companionForge)}</b><code>${esc(f.name)}</code><small>${esc(f.size_label)}</small><div class="cta">${f.mirror_url ? button({ href: f.mirror_url, label: D.mirrorBtn, size: 'sm', primary: true, icon: 'arrow' }) : ''}${extLink(ctx, f.github_url, 'GitHub', { cls: 'gh-alt', fallback: !!f.mirror_url })}</div></div>`).join('')}</div>
</section>` : ''}

<section class="sec" id="install">
 ${secHead({ h: D.installH })}
 <div class="install">
  <ol class="steps">${D.install.map((s) => `<li>${esc(s)}</li>`).join('')}</ol>
  <figure class="card smart">${smart}<figcaption><button type="button" class="btn sm" data-play=".anim-smart">${esc(D.demo)}</button></figcaption></figure>
 </div>
</section>

<section class="sec" id="verify">
 ${secHead({ h: D.verifyH, p: esc(D.verifyP) })}
 <div class="card cmd">${copyable(ctx, `Get-FileHash .\\${st && st.exe ? st.exe.name : 'MineDeck.exe'} -Algorithm SHA256`)}</div>
 <p class="note">${esc(D.verifyNote)}</p>
</section>

<section class="sec" id="requirements">
 ${secHead({ h: D.reqH })}
 <ul class="card list">${D.req.map((s) => `<li>${esc(s)}</li>`).join('')}</ul>
</section>

<section class="sec" id="history">
 ${secHead({ h: D.historyH, more: go(ctx.href('releases'), D.allVersions) })}
 <div class="tbl-wrap card"><table class="files-table"><thead><tr><th>${esc(D.version)}</th><th>${esc(D.channel)}</th><th>${esc(D.date)}</th><th>${esc(L.release.size)}</th><th>${esc(L.release.actions)}</th></tr></thead><tbody>${hist}</tbody></table></div>
</section>

<section class="sec" id="mirror">
 <div class="card notice ok"><b>${esc(D.mirrorH)}</b> ${esc(D.mirrorP)}${data.mirror_placeholder ? ` <em>${esc(D.mirrorPlaceholder)}</em>` : ''}</div>
</section>`;
  return layout(ctx, { title: D.title, desc: D.desc, main });
}
