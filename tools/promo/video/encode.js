// 把 out/frames/ 的 PNG 编成两个 H.264 视频，验证后出联系表和样帧，最后删掉帧图。
//   out/MineDeck-promo-1440p60.mp4   2560×1440（主版本）
//   out/MineDeck-promo-1080p60.mp4   1920×1080（给最高只收 1080p 的平台）
// 编码：High profile、yuv420p、CRF 16、preset slow、BT.709（色彩矩阵在缩放滤镜里就按 709 转，不只是打标签）、
// 恒定 60 fps、+faststart、没有音轨（配乐由 music.js 合进去）。setparams 把原色、传输特性也写成 709（PNG 帧本身不带这两项，
// 只给 -color_primaries / -color_trc 的话新版 ffmpeg 会按帧上的「未指定」写进码流）。
// 用法：node tools/promo/video/encode.js [--keep-frames]
"use strict";
const path = require("path");
const fs = require("fs");
const { execFileSync, spawnSync } = require("child_process");
const P = require("./captions.js");

const HERE = __dirname, OUT = path.join(HERE, "out"), FRAMES = path.join(OUT, "frames");
const TOTAL = Math.round(P.DURATION * P.FPS);
const keep = process.argv.includes("--keep-frames");

const have = fs.existsSync(FRAMES) ? fs.readdirSync(FRAMES).filter(f => /^f_\d{5}\.png$/.test(f)).length : 0;
if (have !== TOTAL) { console.error(`out/frames has ${have} frames, expected ${TOTAL}: run render.js first`); process.exit(1); }

const COMMON = ["-c:v", "libx264", "-profile:v", "high", "-preset", "slow", "-crf", "16", "-g", "120", "-pix_fmt", "yuv420p",
  "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv",
  "-r", "60", "-fps_mode", "cfr", "-movflags", "+faststart", "-an"];
const jobs = [
  { file: "MineDeck-promo-1440p60.mp4", w: 2560, h: 1440, vf: "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv" },
  { file: "MineDeck-promo-1080p60.mp4", w: 1920, h: 1080, vf: "scale=1920:1080:flags=lanczos+accurate_rnd+full_chroma_int:out_color_matrix=bt709:out_range=tv,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv" },
];

function run(cmd, args) {
  console.log("> " + cmd + " " + args.join(" "));
  const r = spawnSync(cmd, args, { stdio: "inherit" });
  if (r.status !== 0) throw new Error(`${cmd} exited with ${r.status}`);
}

const report = [];
for (const j of jobs) {
  const out = path.join(OUT, j.file);
  run("ffmpeg", ["-hide_banner", "-loglevel", "warning", "-stats", "-y", "-framerate", "60", "-i", path.join(FRAMES, "f_%05d.png"), "-vf", j.vf, ...COMMON, out]);
  const probe = JSON.parse(execFileSync("ffprobe", ["-v", "error", "-count_frames", "-select_streams", "v:0", "-show_entries",
    "stream=codec_name,profile,width,height,pix_fmt,r_frame_rate,avg_frame_rate,nb_read_frames,color_space,color_primaries,color_transfer,color_range:format=duration,size,nb_streams",
    "-of", "json", out], { encoding: "utf8" }));
  const s = probe.streams[0], f = probe.format;
  const checks = {
    codec: s.codec_name === "h264", profile: s.profile === "High", size: s.width === j.w && s.height === j.h, pix: s.pix_fmt === "yuv420p",
    fps: s.r_frame_rate === "60/1" && s.avg_frame_rate === "60/1", frames: +s.nb_read_frames === TOTAL, duration: Math.abs(+f.duration - P.DURATION) < 0.02,
    bt709: s.color_space === "bt709" && s.color_primaries === "bt709" && s.color_transfer === "bt709" && s.color_range === "tv", audio: +f.nb_streams === 1,
  };
  const bad = Object.entries(checks).filter(([, ok]) => !ok).map(([k]) => k);
  const line = `${j.file}: ${s.width}x${s.height} ${s.codec_name} ${s.profile} ${s.pix_fmt} ${s.r_frame_rate} fps, ${s.nb_read_frames} frames, ${(+f.duration).toFixed(3)} s, ${(+f.size / 1048576).toFixed(1)} MB, ${s.color_space}/${s.color_primaries}/${s.color_transfer}/${s.color_range}, streams ${f.nb_streams}`;
  console.log(line + (bad.length ? `  FAILED: ${bad.join(", ")}` : "  OK"));
  report.push(line);
  if (bad.length) process.exit(1);
}

// 联系表：每秒一帧，取每秒正中那一帧（第 30、90、150… 帧，避开落在小节线上的转场），8 列
run("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", "-i", path.join(OUT, jobs[0].file), "-vf", "select='eq(mod(n\\,60)\\,30)',scale=480:-1:flags=lanczos,tile=8x10:padding=6:margin=6:color=white", "-frames:v", "1", "-update", "1", path.join(OUT, "contact-sheet.png")]);
// 样帧（原尺寸 PNG，直接从帧图里拷）
const SAMPLES = [4.9, 22.8, 29.6, 38.5, 49.6, 53.8, 70];
fs.mkdirSync(path.join(OUT, "sample-frames"), { recursive: true });
for (const t of SAMPLES) {
  const n = Math.round(t * 60);
  fs.copyFileSync(path.join(FRAMES, `f_${String(n).padStart(5, "0")}.png`), path.join(OUT, "sample-frames", `t${t.toFixed(1).replace(".", "_")}s-f${n}.png`));
}
fs.writeFileSync(path.join(OUT, "ffprobe.txt"), report.join("\n") + "\n");
if (!keep) { fs.rmSync(FRAMES, { recursive: true, force: true }); console.log("deleted out/frames (the PNG frames); pass --keep-frames to keep them"); }
console.log("done: " + jobs.map(j => path.join(OUT, j.file)).join(", "));
