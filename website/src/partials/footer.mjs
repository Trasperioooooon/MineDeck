// 页脚（ARCH 3.3）：四组链接 + 备案号占位、免责声明、字体许可、数据更新时间。
import { esc, fmt } from './util.mjs';
import { extLink } from './ui.mjs';

const get = (L, path) => path.split('.').reduce((o, k) => o && o[k], L);

export function footer(ctx) {
  const { L, cfg, page, data } = ctx;
  const icp = cfg.beian.icp ? `<a href="https://beian.miit.gov.cn/" rel="noopener">${esc(cfg.beian.icp)}</a>` : esc(L.footer.pending);
  const psb = cfg.beian.psb
    ? `<a href="https://beian.mps.gov.cn/#/query/webSearch?code=${esc(cfg.beian.psbCode)}" rel="noopener">${esc(cfg.beian.psb)}</a>`
    : esc(L.footer.pending);
  const link = (n) => n.ext ? extLink(ctx, n.ext(cfg), get(L, n.label))
    : n.file ? `<a href="${ctx.root}${n.file}">${esc(get(L, n.label))}</a>`
    : `<a href="${ctx.href(n.route)}${n.hash ? '#' + n.hash : ''}">${esc(get(L, n.label))}</a>`;
  const groups = ctx.footer.map((g) => `<div class="foot-col"><b>${esc(L.footer[g.h])}</b>${g.links.map(link).join('')}</div>`).join('');
  const langCol = page.noAlt ? '' : `<div class="foot-col"><b>${esc(L.footer.language)}</b><a href="${ctx.link(page.urls.zh)}" lang="zh-CN" hreflang="zh-CN" data-set-lang="zh">中文</a><a href="${ctx.link(page.urls.en)}" lang="en" hreflang="en" data-set-lang="en">English</a></div>`;
  const asOf = fmt(L.footer.dataAsOf, { date: new Date(Date.parse(data.generated_at) + cfg.timezoneOffsetHours * 3600e3).toISOString().slice(0, 16).replace('T', ' ') });
  return `<div class="dirt" aria-hidden="true"></div>
<footer><div class="wrap">
 <div class="foot-nav">${groups}${langCol}</div>
 <div class="row"><span>${esc(L.footer.copyright)}</span><span>${fmt(esc(L.footer.icp), { v: icp })}</span><span>${fmt(esc(L.footer.psb), { v: psb })}</span></div>
 <p>${esc(L.footer.disclaimer)}</p>
 <p>${esc(L.footer.fonts)} <a href="${ctx.link('/assets/licenses/')}">${esc(L.footer.fontLicenses)}</a> · ${esc(asOf)}</p>
</div></footer>
</body></html>
`;
}
