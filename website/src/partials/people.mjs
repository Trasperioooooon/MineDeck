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

/**
 * 作者和贡献者分两栏：仓库所有者（site.config.json 里 repo 的前半段）单独一栏，标「作者」；
 * 其余提交过改动的人在另一栏，标「贡献者」，最多 max 个，超出显示 +N。没有其他贡献者时只有作者一栏。
 */
export function peopleSplit(ctx, { max = 12, showCount = false } = {}) {
  const { L, cfg, data } = ctx;
  const owner = cfg.repo.split('/')[0].toLowerCase();
  const list = data.contributors.list;
  const authors = list.filter((c) => c.login.toLowerCase() === owner);
  const others = list.filter((c) => c.login.toLowerCase() !== owner);
  const extra = others.length - max;
  const col = (label, body) => `<div class="people-col"><b>${esc(label)}</b><div class="avatars">${body}</div></div>`;
  const more = extra > 0 ? `<a class="avatar more" href="${ctx.href('about')}#contributors"><span class="avatar-ph">${esc(fmt(L.home.moreContributors, { n: extra }))}</span></a>` : '';
  return `<div class="people">${authors.length ? col(L.people.author, authors.map((c) => avatar(ctx, c, { showCount })).join('')) : ''}${others.length ? col(L.people.contributors, others.slice(0, max).map((c) => avatar(ctx, c, { showCount })).join('') + more) : ''}</div>`;
}
