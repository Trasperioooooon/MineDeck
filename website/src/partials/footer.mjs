// 页脚：版权、备案号占位、免责声明、字体许可。
import { esc, fmt } from './util.mjs';

export function footer(ctx) {
  const { L, cfg } = ctx;
  const icp = cfg.beian.icp ? `<a href="https://beian.miit.gov.cn/" rel="noopener">${esc(cfg.beian.icp)}</a>` : esc(L.footer.pending);
  const psb = cfg.beian.psb
    ? `<a href="https://beian.mps.gov.cn/#/query/webSearch?code=${esc(cfg.beian.psbCode)}" rel="noopener">${esc(cfg.beian.psb)}</a>`
    : esc(L.footer.pending);
  const links = ctx.footerLinks.map((n) => `<a href="${esc(n.ext ? n.ext(cfg) : ctx.href(n.route))}">${esc(n.text || L.nav[n.label])}</a>`).join('');
  return `<div class="dirt" aria-hidden="true"></div>
<footer><div class="wrap">
 <div class="row"><span>${esc(L.footer.copyright)}</span><span>${fmt(esc(L.footer.icp), { v: icp })}</span><span>${fmt(esc(L.footer.psb), { v: psb })}</span>
 ${links}</div>
 <p>${esc(L.footer.disclaimer)}</p>
 <p>${esc(L.footer.fonts)}</p>
</div></footer>
</body></html>
`;
}
