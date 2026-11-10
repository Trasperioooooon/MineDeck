// 从 data/ 下的原始数据生成页面模型：版本（含上一版/下一版、分组、文件说明）、最新版本、统计、贡献者、活动。
import { render, sanitize, summary } from './markdown.mjs';

const DAY = 86400e3;
const sizeLabel = (n) => (n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : Math.max(1, Math.round(n / 1024)) + ' KB');

/** 文件种类，用于下载格的说明文字和排序 */
function fileKind(name) {
  if (/\.exe$/i.test(name)) return 'exe';
  if (/companion-neoforge/i.test(name)) return 'companion-neoforge';
  if (/companion-forge/i.test(name)) return 'companion-forge';
  if (/^SHA256SUMS/i.test(name)) return 'sums';
  return 'other';
}
const KIND_ORDER = ['exe', 'companion-neoforge', 'companion-forge', 'sums', 'other'];

/**
 * 正文里的 GitHub 链接：指向本仓库某个已知版本的，改成站内占位 md-release:<tag>（渲染时换成相对链接）；
 * 其余 GitHub 链接标成外链（data-ext）。h4 加锚点，便于生成本页目录。
 */
function localizeBody(html, repo, tags) {
  const relRe = new RegExp(`^https://github\\.com/${repo.replace('/', '\\/')}/releases/tag/([^"#?]+)$`);
  const toc = [];
  let n = 0;
  html = html.replace(/<a href="([^"]*)" rel="noopener nofollow">/g, (m, href) => {
    const raw = href.replace(/&amp;/g, '&');
    const rm = raw.match(relRe);
    if (rm && tags.has(decodeURIComponent(rm[1]))) return `<a href="md-release:${decodeURIComponent(rm[1])}">`;
    if (/^https:\/\/([\w-]+\.)?github(usercontent)?\.com\//.test(raw)) return `<a class="ext" data-ext href="${href}" target="_blank" rel="noopener nofollow">`;
    return m;
  });
  html = html.replace(/<h4>([\s\S]*?)<\/h4>/g, (m, inner) => {
    const id = `n-${++n}`;
    toc.push({ id, text: inner.replace(/<[^>]+>/g, '') });
    return `<h4 id="${id}">${inner}</h4>`;
  });
  return { html, toc };
}

export function buildModel({ raw, repoInfo, contributors, activity, cfg, generatedAt }) {
  const off = cfg.timezoneOffsetHours * 3600e3;
  const localTime = (iso) => new Date(Date.parse(iso) + off).toISOString().slice(0, 16).replace('T', ' ');
  const localDay = (t) => new Date(t + off).toISOString().slice(0, 10);
  const mirrorUrl = (tag, file) => (cfg.mirror && cfg.mirror.enabled ? cfg.mirror.template.replace('{tag}', encodeURIComponent(tag)).replace('{file}', encodeURIComponent(file)) : null);
  const list = raw.filter((r) => !r.draft && r.published_at).sort((a, b) => Date.parse(b.published_at) - Date.parse(a.published_at));
  const tags = new Set(list.map((r) => r.tag_name));

  const releases = list.map((r) => {
    const assets = r.assets.map((a) => ({
      name: a.name, size: a.size, size_label: sizeLabel(a.size), kind: fileKind(a.name),
      sha256: a.digest && a.digest.startsWith('sha256:') ? a.digest.slice(7) : null,
      github_url: a.browser_download_url, mirror_url: mirrorUrl(r.tag_name, a.name),
    })).sort((a, b) => KIND_ORDER.indexOf(a.kind) - KIND_ORDER.indexOf(b.kind));
    const exe = assets.find((a) => a.kind === 'exe') || null;
    const body = localizeBody(sanitize(render(r.body || '')), cfg.repo, tags);
    const major = (r.tag_name.match(/^v?(\d+)\.(\d+)/) || []).slice(1, 3).join('.');
    const legacy = !!(exe && /^AutoMCHUB/i.test(exe.name));
    return {
      tag: r.tag_name, name: r.name || r.tag_name, prerelease: !!r.prerelease, channel: r.prerelease ? 'beta' : 'stable',
      published_at: r.published_at, date_local: localTime(r.published_at), day_local: localDay(Date.parse(r.published_at)),
      html_url: r.html_url, legacy, group: legacy ? 'legacy' : major,
      summary: summary(r.body), body_html: body.html, toc: body.toc, assets, exe,
    };
  });
  // 上一版/下一版（按时间）与同渠道上一版；距上一版天数；有无更新的同渠道版本
  releases.forEach((r, i) => {
    r.prev = releases[i + 1] ? releases[i + 1].tag : null;
    r.next = releases[i - 1] ? releases[i - 1].tag : null;
    const sameOlder = releases.slice(i + 1).find((x) => x.channel === r.channel);
    r.prev_same = sameOlder ? sameOlder.tag : null;
    const newerSame = releases.slice(0, i).find((x) => x.channel === r.channel);
    r.newer_same = newerSame ? newerSame.tag : null;
    r.days_since_prev = releases[i + 1] ? Math.round((Date.parse(r.published_at) - Date.parse(releases[i + 1].published_at)) / DAY) : null;
    r.compare_url = releases[i + 1] && !r.legacy && !releases[i + 1].legacy ? `https://github.com/${cfg.repo}/compare/${releases[i + 1].tag}...${r.tag}` : null;
  });
  // 分组：大版本（3.1、3.0、2.9…），AutoMCHUB 时期单独一组
  const groups = [];
  for (const r of releases) {
    let g = groups.find((x) => x.key === r.group);
    if (!g) groups.push((g = { key: r.group, legacy: r.legacy, releases: [] }));
    g.releases.push(r);
  }
  const latest_stable = releases.find((r) => !r.prerelease) || null;
  const latest_beta = releases.find((r) => r.prerelease) || null;
  const renamed = [...releases].reverse().find((r) => r.exe && /^MineDeck/i.test(r.exe.name));

  // 活动：每天提交数 + 发布日
  const relByDay = {};
  for (const r of releases) (relByDay[r.day_local] ||= []).push(r.tag);

  const exclude = new Set((cfg.contributors && cfg.contributors.exclude) || []);
  const people = ((contributors && contributors.list) || []).filter((c) => !exclude.has(c.login));
  const hidden = ((contributors && contributors.list) || []).length - people.length;

  return {
    generated_at: generatedAt, repo: cfg.repo, timezone: `UTC+${cfg.timezoneOffsetHours}`, mirror_placeholder: !!(cfg.mirror && cfg.mirror.enabled),
    latest_stable, latest_beta, show_beta: !!(latest_beta && (!latest_stable || Date.parse(latest_beta.published_at) > Date.parse(latest_stable.published_at))),
    releases, groups, byTag: Object.fromEntries(releases.map((r) => [r.tag, r])), renamed_in: renamed ? renamed.tag : null,
    repoInfo: repoInfo || null,
    contributors: { fetched_at: contributors && contributors.fetched_at, list: people, hidden },
    activity: { fetched_at: activity && activity.fetched_at, source: activity && activity.source, days: (activity && activity.days) || {}, releases: relByDay },
    localDay,
  };
}

/** 页面里公开的精简数据 */
export const pickRelease = (r) => r && { tag: r.tag, channel: r.channel, published_at: r.published_at, exe: r.exe && { name: r.exe.name, size: r.exe.size, sha256: r.exe.sha256, mirror_url: r.exe.mirror_url, github_url: r.exe.github_url } };
