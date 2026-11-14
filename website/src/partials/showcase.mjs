// 首页亮点展示台：左边列出几个亮点的名字，右边是当前这一项的面板（说明 + 演示）。
// 不自动翻页，点哪个换哪个。没有 JS 或窄屏时，所有面板依次排开，左边的名字就是跳到各面板的链接（见 pages.css、core.js）。
// 面板里放什么由 content/ui.*.json 的 home.show.items 决定，三种之一：
//   demo  内联 SVG 小演示（src/svg/<demo>.svg，文字从 text 传入，动画在 motion.css）
//   shot  真实界面截图（docs/images 里的文件名，替代文字取自 shots.items）
//   steps 三步文字说明（还没有演示和截图的功能先用这个）
import { esc } from './util.mjs';
import { go } from './ui.mjs';
import { icon, illustration } from './svg.mjs';

export function showcase(ctx) {
  const { L } = ctx;
  const S = L.home.show;
  const badge = `<span class="badge bt">${esc(S.beta)}</span>`;
  const shotAlt = (f) => (L.shots.items.find((s) => s.f === f) || {}).alt || '';
  const tabs = S.items.map((it, i) =>
    `<a class="sc-tab" href="#sc-${esc(it.id)}" data-sc="${esc(it.id)}"${i === 0 ? ' aria-current="true"' : ''}><span>${esc(it.t)}</span>${it.beta ? badge : ''}</a>`).join('\n  ');
  const panels = S.items.map((it) => {
    const id = `sc-${it.id}`;
    const art = it.demo
      ? `<div class="sc-art">${illustration(it.demo, { label: it.aria, text: it.text })}</div>`
      : it.shot
        ? `<figure class="sc-art sc-shot"><img src="${ctx.a(it.shot)}" alt="${esc(shotAlt(it.shot))}" width="1440" height="900" loading="lazy"><figcaption>${esc(S.shotNote)}</figcaption></figure>`
        : `<div class="sc-art sc-steps"><span class="ico">${icon(it.icon || 'block', { cls: 'feat-ico' })}</span><ol class="steps">${it.steps.map((s) => `<li>${esc(s)}</li>`).join('')}</ol></div>`;
    return `<section class="sc-panel card" id="${esc(id)}">
   <h3 class="sc-h">${esc(it.t)}${it.beta ? ' ' + badge : ''}</h3>
   <p class="sc-d">${esc(it.d)}${it.beta ? ` ${esc(S.betaNote)}` : ''}</p>
   ${art}
   <p class="sc-links">${go(ctx.href('features') + '#' + it.more, S.moreLink)}${it.beta ? go(ctx.href('download'), S.betaLink) : ''}${it.demo ? `<button type="button" class="btn sm js-only sc-replay" data-play="#${esc(id)} .sc-demo">${esc(S.replay)}</button>` : ''}</p>
  </section>`;
  }).join('\n  ');
  return `<div class="showcase">
 <nav class="sc-tabs" aria-label="${esc(S.tabsAria)}">
  ${tabs}
 </nav>
 <div class="sc-stage">
  ${panels}
 </div>
</div>`;
}
