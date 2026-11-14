// fetch 步骤：联网取 GitHub 数据并落地到 data/，供离线、确定性的 build 使用。
// 浏览器运行时不会请求 GitHub；这里是唯一联网的地方。
import { writeFileSync, mkdirSync, existsSync, readFileSync, readdirSync, rmSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { join } from 'node:path';

const API = 'https://api.github.com';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function client() {
  const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  const headers = { Accept: 'application/vnd.github+json', 'X-GitHub-Api-Version': '2022-11-28', 'User-Agent': 'minedeck-website-build', ...(token ? { Authorization: `Bearer ${token}` } : {}) };
  return async function gh(path, { allow202 = false } = {}) {
    const res = await fetch(API + path, { headers });
    if (res.status === 202 && allow202) return { pending: true };
    if (!res.ok) throw new Error(`GitHub API ${path} → ${res.status}: ${(await res.text()).slice(0, 200)}`);
    return res.json();
  };
}

async function releases(gh, repo) {
  const all = [];
  for (let page = 1; page < 20; page++) {
    const list = await gh(`/repos/${repo}/releases?per_page=100&page=${page}`);
    all.push(...list);
    if (list.length < 100) break;
  }
  // 只留构建用得到的字段
  return all.map((r) => ({
    tag_name: r.tag_name, name: r.name, draft: r.draft, prerelease: r.prerelease, published_at: r.published_at, html_url: r.html_url, body: r.body,
    assets: (r.assets || []).map((a) => ({ name: a.name, size: a.size, digest: a.digest || null, browser_download_url: a.browser_download_url })),
  }));
}

/** 头像：96px，PIL 可用时重新压缩为调色板 PNG；失败返回 false（build 时用像素占位头像） */
async function avatar(url, file) {
  try {
    const res = await fetch(url + (url.includes('?') ? '&' : '?') + 's=96');
    if (!res.ok) return false;
    writeFileSync(file, Buffer.from(await res.arrayBuffer()));
    const py = `from PIL import Image\nim=Image.open(${JSON.stringify(file)}).convert('RGBA').resize((96,96))\nim.convert('RGB').quantize(64).save(${JSON.stringify(file)},optimize=True)`;
    spawnSync('python3', ['-c', py], { stdio: 'ignore' }); // 没有 PIL 时保留原图
    return true;
  } catch { return false; }
}

/** 近 52 周每日提交数。首选本地 git log（需要完整历史），否则用 stats/commit_activity（可能 202，重试） */
async function activity(gh, repo, repoRoot, tzHours, branch) {
  // 统计默认分支（CI 里 checkout 的就是它；本地开发分支上的提交不计入）
  const ref = [`origin/${branch}`, 'HEAD'].find((x) => spawnSync('git', ['-C', repoRoot, 'rev-parse', '--verify', '-q', x], { stdio: 'ignore' }).status === 0) || 'HEAD';
  const since = new Date(Date.now() - 371 * 86400e3).toISOString();
  const r = spawnSync('git', ['-C', repoRoot, 'log', '--no-merges', `--since=${since}`, '--format=%aI', ref], { encoding: 'utf8' });
  const shallow = spawnSync('git', ['-C', repoRoot, 'rev-parse', '--is-shallow-repository'], { encoding: 'utf8' }).stdout.trim() === 'true';
  if (r.status === 0 && !shallow) {
    const days = {};
    for (const line of r.stdout.split('\n').filter(Boolean)) {
      const k = new Date(Date.parse(line) + tzHours * 3600e3).toISOString().slice(0, 10);
      days[k] = (days[k] || 0) + 1;
    }
    return { source: `git log --no-merges ${ref}`, days };
  }
  for (let i = 0; i < 6; i++) {
    const weeks = await gh(`/repos/${repo}/stats/commit_activity`, { allow202: true });
    if (!weeks.pending && Array.isArray(weeks)) {
      const days = {};
      for (const w of weeks) w.days.forEach((n, d) => { if (n) days[new Date((w.week + d * 86400) * 1000).toISOString().slice(0, 10)] = n; });
      return { source: 'GitHub stats/commit_activity', days };
    }
    await sleep(3000 * (i + 1));
  }
  throw new Error('commit_activity 一直返回 202');
}

/**
 * 拉取全部数据写进 dataDir。单项失败只警告，保留旧文件。
 * @returns {Promise<boolean>} 是否全部成功
 */
export async function fetchAll({ cfg, dataDir, repoRoot, log }) {
  const gh = client();
  const repo = cfg.repo;
  const now = new Date().toISOString();
  const save = (name, obj) => writeFileSync(join(dataDir, name), JSON.stringify(obj, null, 1) + '\n');
  mkdirSync(join(dataDir, 'avatars'), { recursive: true });
  let ok = true;
  const step = async (name, fn) => {
    try { await fn(); } catch (e) { ok = false; log(`警告：${name} 拉取失败（${e.message}），继续使用已有的 data/ 文件`); }
  };
  await step('releases', async () => { const r = await releases(gh, repo); save('releases.cache.json', r); log(`releases：${r.length} 个`); });
  await step('repo', async () => {
    const r = await gh(`/repos/${repo}`);
    save('repo.json', { fetched_at: now, stargazers_count: r.stargazers_count, forks_count: r.forks_count, open_issues_count: r.open_issues_count, license: r.license && r.license.spdx_id, pushed_at: r.pushed_at, default_branch: r.default_branch });
    log(`repo：${r.stargazers_count} stars`);
  });
  await step('contributors', async () => {
    const list = await gh(`/repos/${repo}/contributors?per_page=100`);
    const exclude = new Set((cfg.contributors && cfg.contributors.exclude) || []);
    const people = list.filter((c) => c.type !== 'Bot' && !exclude.has(c.login));
    const keep = new Set();
    for (const c of people) {
      const f = join(dataDir, 'avatars', `${c.login}.png`);
      c.avatar = (await avatar(c.avatar_url, f)) ? `avatars/${c.login}.png` : null;
      keep.add(`${c.login}.png`);
    }
    for (const f of readdirSync(join(dataDir, 'avatars'))) if (!keep.has(f)) rmSync(join(dataDir, 'avatars', f));
    save('contributors.json', { fetched_at: now, list: people.map((c) => ({ login: c.login, contributions: c.contributions, html_url: c.html_url, avatar: c.avatar })) });
    log(`contributors：${people.map((c) => c.login).join('、')}`);
  });
  await step('activity', async () => {
    const a = await activity(gh, repo, repoRoot, cfg.timezoneOffsetHours, readJson(dataDir, 'repo.json', {}).default_branch || 'main');
    save('activity.json', { fetched_at: now, timezone: `UTC+${cfg.timezoneOffsetHours}`, ...a });
    log(`activity：${Object.values(a.days).reduce((s, n) => s + n, 0)} 次提交（${a.source}）`);
  });
  return ok;
}

export const hasData = (dataDir) => existsSync(join(dataDir, 'releases.cache.json'));
export const readJson = (dataDir, name, fallback) => (existsSync(join(dataDir, name)) ? JSON.parse(readFileSync(join(dataDir, name), 'utf8')) : fallback);
