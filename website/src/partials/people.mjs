// 贡献者头像。头像在构建时下载到本地并带 hash；没有头像时用按 login 取色的像素占位。
import { esc, fmt } from './util.mjs';
import { extLink } from './ui.mjs';

const hue = (s) => [...s].reduce((h, c) => (h * 31 + c.charCodeAt(0)) >>> 0, 7) % 12;

export function avatar(ctx, c, { showCount } = {}) {
  const img = c.avatarAsset
    ? `<img src="${ctx.a(c.avatarAsset)}" alt="" width="52" height="52" loading="lazy">`
    : `<span class="avatar-ph" style="--c:var(--icon-${hue(c.login)})" aria-hidden="true">${esc(c.login[0].toUpperCase())}</span>`;
  const body = `${img}<span>${esc(c.login)}</span>${showCount ? `<small>${esc(fmt(ctx.L.about.commits, { n: c.contributions }))}</small>` : ''}`;
  return extLink(ctx, c.html_url, '', { cls: 'avatar', html: body, aria: c.login });
}

/** 一排头像，最多 max 个，超出显示 +N */
export function avatarRow(ctx, { max = 12, showCount = false } = {}) {
  const list = ctx.data.contributors.list;
  const extra = list.length - max;
  return `<div class="avatars">${list.slice(0, max).map((c) => avatar(ctx, c, { showCount })).join('')}${extra > 0 ? `<a class="avatar more" href="${ctx.href('about')}#contributors"><span class="avatar-ph">${esc(fmt(ctx.L.home.moreContributors, { n: extra }))}</span></a>` : ''}</div>`;
}
