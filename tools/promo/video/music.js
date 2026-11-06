// 把配乐合进 out/ 里的两个视频：视频流原样拷贝，只加一条 AAC 音轨（48 kHz 立体声、256 kbps），就地替换。
// 配乐是 music/Warmth_by_the_Window.mp3，作者用 Gemini 的音乐模型按「配乐提示词.md」生成、2026-11-06 选定。
// 曲子实际是 124 BPM，开头也比画面长：律动在 7.932 秒进来，主段落在 46.05 秒落下，两处前面都有一小段空白。
// 画面上对应的是第 3 小节第 1 拍（4 秒，图标落定）和第 23 小节第 1 拍（44 秒，「AI 是可选的」）。
// 对齐办法：整首按采样率放慢（asetrate，像唱片放慢，音高跟着低不到一个半音，但没有时间拉伸的杂音），
// 让两处的间隔从 38.118 秒变成 40 秒，再裁掉开头，让第一下落在 4 秒。曲子自己在 75 秒前渐弱到无声，
// 开头补 0.5 秒淡入、结尾补 1 秒淡出，截到 76 秒。响度整体线性调到 -14 LUFS，真峰值不超过 -1.5 dBTP。
// 换曲子：改 MUSIC 和 HIT。HIT 是两处空白后起音的位置，用 2–4 毫秒一格的响度包络找。
// 用法：node tools/promo/video/music.js（先跑过 encode.js）
"use strict";
const path = require("path");
const fs = require("fs");
const { execFileSync, spawnSync } = require("child_process");
const P = require("./captions.js");

const HERE = __dirname, OUT = path.join(HERE, "out");
const MUSIC = path.join(HERE, "music", "Warmth_by_the_Window.mp3"), SRC_RATE = 44100;
const HIT = [7.932, 46.05], AT = [P.T(3), P.T(23)];
const TARGET_I = -14, CEIL_TP = -1.5;
const FILES = ["MineDeck-promo-1440p60.mp4", "MineDeck-promo-1080p60.mp4"];
const WAV = path.join(OUT, "music-48k.wav");

const rate = Math.round(SRC_RATE * (HIT[1] - HIT[0]) / (AT[1] - AT[0])); // asetrate 只收整数
const slow = SRC_RATE / rate;
const head = HIT[0] - AT[0] / slow; // 曲子开头裁掉的秒数（曲子自己的时间）
const D = P.DURATION;
const chain = [
  "asetpts=PTS-STARTPTS", `atrim=start=${head.toFixed(5)}`, "asetpts=PTS-STARTPTS",
  `asetrate=${rate}`, "aresample=48000",
  "afade=t=in:d=0.5", `afade=t=out:st=${D - 1}:d=1`, `apad=whole_dur=${D}`, `atrim=duration=${D}`,
].join(",");

function run(cmd, args) {
  console.log("> " + cmd + " " + args.join(" "));
  const r = spawnSync(cmd, args, { stdio: "inherit" });
  if (r.status !== 0) throw new Error(`${cmd} exited with ${r.status}`);
}

// loudnorm 只用来量：它在结尾把 JSON 打到 stderr
function measure(input, af) {
  const r = spawnSync("ffmpeg", ["-hide_banner", "-nostats", "-i", input, "-af", `${af ? af + "," : ""}loudnorm=print_format=json`, "-f", "null", "-"], { encoding: "utf8" });
  if (r.status !== 0) throw new Error("ffmpeg loudness measure failed:\n" + r.stderr);
  const j = JSON.parse(r.stderr.match(/\{[^{}]*"input_i"[^{}]*\}/)[0]);
  return { I: +j.input_i, TP: +j.input_tp };
}

for (const f of FILES) if (!fs.existsSync(path.join(OUT, f))) { console.error(`out/${f} is missing: run encode.js first`); process.exit(1); }
if (!fs.existsSync(MUSIC)) { console.error(`${MUSIC} is missing`); process.exit(1); }
console.log(`slow down x${slow.toFixed(5)} (asetrate ${rate}, ${(12 * Math.log2(slow)).toFixed(2)} semitones lower), cut ${head.toFixed(3)} s from the start`);

const m = measure(MUSIC, chain);
const gain = Math.min(TARGET_I - m.I, CEIL_TP - m.TP);
console.log(`before: ${m.I} LUFS, ${m.TP} dBTP -> gain ${gain.toFixed(2)} dB`);
run("ffmpeg", ["-hide_banner", "-loglevel", "warning", "-y", "-i", MUSIC, "-af", `${chain},volume=${gain.toFixed(2)}dB`, "-c:a", "pcm_s24le", WAV]);
const after = measure(WAV);
console.log(`after: ${after.I} LUFS, ${after.TP} dBTP`);

const report = [];
for (const f of FILES) {
  const out = path.join(OUT, f), tmp = path.join(OUT, ".mux-" + f);
  run("ffmpeg", ["-hide_banner", "-loglevel", "warning", "-y", "-i", out, "-i", WAV, "-map", "0:v:0", "-map", "1:a:0",
    "-c:v", "copy", "-c:a", "aac", "-b:a", "256k", "-ar", "48000", "-ac", "2", "-movflags", "+faststart", tmp]);
  fs.renameSync(tmp, out);
  const probe = JSON.parse(execFileSync("ffprobe", ["-v", "error", "-show_entries",
    "stream=codec_type,codec_name,nb_frames,sample_rate,channels,duration:format=duration,size,nb_streams", "-of", "json", out], { encoding: "utf8" }));
  const v = probe.streams.find(s => s.codec_type === "video"), a = probe.streams.find(s => s.codec_type === "audio"), fm = probe.format;
  const ok = +fm.nb_streams === 2 && v && v.codec_name === "h264" && +v.nb_frames === Math.round(D * P.FPS) &&
    a && a.codec_name === "aac" && a.sample_rate === "48000" && a.channels === 2 && Math.abs(+a.duration - D) < 0.05;
  const line = `${f}: + audio ${a ? `${a.codec_name} ${a.sample_rate} Hz ${a.channels} ch ${(+a.duration).toFixed(3)} s` : "missing"}, ` +
    `video ${v ? v.nb_frames : "?"} frames, ${(+fm.size / 1048576).toFixed(1)} MB, music ${after.I} LUFS / ${after.TP} dBTP`;
  console.log(line + (ok ? "  OK" : "  FAILED"));
  if (!ok) process.exit(1);
  report.push(line);
}

const probeTxt = path.join(OUT, "ffprobe.txt");
const prev = fs.existsSync(probeTxt) ? fs.readFileSync(probeTxt, "utf8").split("\n").filter(l => l && !l.includes(": + audio ")) : [];
fs.writeFileSync(probeTxt, [...prev, ...report].join("\n") + "\n");
console.log("done: " + FILES.map(f => path.join(OUT, f)).join(", "));
