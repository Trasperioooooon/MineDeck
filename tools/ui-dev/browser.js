// 启动测试和截图用的浏览器。
// Playwright 自带的 Chromium 版本跟着 playwright 包走，本机可能装的是别的版本（npx playwright install 在别处装的），
// 也可能根本没装：找不到就改用本机装的 Google Chrome（channel: "chrome"），不用为测试再下载一份浏览器。
// 想指定的话：CHROME=<chrome.exe 的完整路径> 或 PW_CHANNEL=chrome / msedge。
"use strict";
const fs = require("fs");
const { chromium } = require("playwright");

async function launch(opts = {}) {
  if (process.env.CHROME) return chromium.launch({ ...opts, executablePath: process.env.CHROME });
  if (process.env.PW_CHANNEL) return chromium.launch({ ...opts, channel: process.env.PW_CHANNEL });
  let bundled = "";
  try { bundled = chromium.executablePath(); } catch { /* 没装过任何浏览器时这里会抛 */ }
  if (bundled && fs.existsSync(bundled)) return chromium.launch(opts);
  return chromium.launch({ ...opts, channel: "chrome" });
}

module.exports = { launch };
