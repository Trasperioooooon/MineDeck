// 录演示视频的各段。每段一个进程（各自一份仿真后端和 Chrome），并行跑。
// 用法：
//   node tools/promo/tutorial/record.js                 录全部，输出 out/seg/<段号>.mkv 和 .json
//   node tools/promo/tutorial/record.js --only=02,03    只录这几段
//   node tools/promo/tutorial/record.js --preview=30    看片用：每 30 帧存一张半尺寸的图到 out/preview/<段号>/，很快
//   --jobs=4 同时跑几段
"use strict";
const path = require("path");
const fs = require("fs");
const { spawn } = require("child_process");
const HERE = __dirname;
const arg = (k, d) => { const a = process.argv.find(x => x.startsWith(`--${k}=`)); return a ? a.slice(k.length + 3) : d; };

async function one(id, preview) {
  const { Director } = require("./director.js");
  const seg = require("./scenes.js").find(s => s.id === id);
  if (!seg) throw new Error("no such segment: " + id);
  const out = preview ? path.join(HERE, "out", "preview", id) : path.join(HERE, "out", "seg", id + ".mkv");
  if (preview) { fs.rmSync(out, { recursive: true, force: true }); fs.mkdirSync(out, { recursive: true }); }
  const d = new Director({ out, preview });
  const t0 = Date.now();
  try {
    await d.boot(seg.boot);
    await seg.run(d);
  } catch (e) {
    // 卡住的时候留一张当时的画面，看得出停在哪
    try { fs.mkdirSync(path.join(HERE, "out", "fail"), { recursive: true }); fs.writeFileSync(path.join(HERE, "out", "fail", id + ".png"), await d.shot(0.5)); } catch { /* 浏览器已经没了 */ }
    throw e;
  }
  const r = await d.finish();
  if (!preview) fs.writeFileSync(path.join(HERE, "out", "seg", id + ".json"), JSON.stringify({ id, title: seg.title, ...r }, null, 1));
  console.log(`${id} ${seg.title}: ${r.frames} frames, ${(r.frames / 60).toFixed(1)} s, ${((Date.now() - t0) / 1000).toFixed(0)} s real`);
}

async function all() {
  const scenes = require("./scenes.js");
  const only = arg("only", "");
  const ids = scenes.map(s => s.id).filter(id => !only || only.split(",").includes(id));
  const preview = arg("preview", "");
  const jobs = +arg("jobs", 4);
  let next = 0, bad = 0;
  const log = path.join(HERE, "out", "record.log");
  fs.mkdirSync(path.dirname(log), { recursive: true });
  const worker = async () => {
    while (next < ids.length) {
      const id = ids[next++];
      await new Promise(res => {
        const args = [__filename, "--seg=" + id]; if (preview) args.push("--preview=" + preview);
        const p = spawn(process.execPath, args, { stdio: "inherit" });
        p.on("close", c => { if (c) { bad++; console.error(`${id} failed`); } res(); });
      });
    }
  };
  await Promise.all(Array.from({ length: jobs }, worker));
  if (bad) process.exit(1);
}

const seg = arg("seg", "");
(seg ? one(seg, +arg("preview", 0)) : all()).catch(e => { console.error(e); process.exit(1); });
