// 把配乐合进 out/ 里的两个视频：视频流原样拷贝，只加一条 AAC 音轨（48 kHz 立体声、256 kbps），就地替换，可以反复跑。
// 配乐是 music/Morning_on_the_Balcony.mp3，作者 2026-11-12 按「配乐提示词.md」用 Gemini 的音乐模型生成。
// analyze-music.py 量过：123.99 BPM，速度很稳（每 16 拍的偏差在 8 毫秒以内），第一拍在 0.404 秒，小节线从 1.372 秒起每 1.93564 秒一条；
// 7 小节前奏（第 5 小节低音进来）、16 小节律动（14.92 秒鼓进来）、4 小节安静的过渡（45.89 秒）、16 小节主段落（53.63 秒落下）、
// 2 小节收尾，89 秒前收尽。画面的节拍网格就是按这个速度排的（captions.js），所以曲子不变速，只从第 3 小节（5.2431 秒）开始截：
// 画面 0 秒 = 曲子 5.2431 秒。开头 0.05 秒淡入（免得切口有咔哒声），截到 84 秒，最后 0.5 秒淡出（曲子自己这时已经收尽）。
// 响度整体线性调到 -14 LUFS，真峰值不超过 -1.5 dBTP。
// 换曲子：先跑 analyze-music.py 量速度和段落，改 captions.js 的 BPM、MUSIC_START 和各段落的小节，再改这里的 MUSIC。
// 用法：node tools/promo/video-v3.1/music.js（先跑过 encode.js）
"use strict";
const path = require("path");
const fs = require("fs");
const { execFileSync, spawnSync } = require("child_process");
const P = require("./captions.js");

const HERE = __dirname, OUT = path.join(HERE, "out");
const MUSIC = path.join(HERE, "music", "Morning_on_the_Balcony.mp3");
const TARGET_I = -14, CEIL_TP = -1.5;
const FILES = ["MineDeck-v3.1-promo-1440p60.mp4", "MineDeck-v3.1-promo-1080p60.mp4"];
const WAV = path.join(OUT, "music-48k.wav");
const D = P.DURATION;
const chain = [
  "asetpts=PTS-STARTPTS", `atrim=start=${P.MUSIC_START.toFixed(4)}`, "asetpts=PTS-STARTPTS", "aresample=48000",
  "afade=t=in:d=0.05", `afade=t=out:st=${(D - 0.5).toFixed(3)}:d=0.5`, `apad=whole_dur=${D}`, `atrim=duration=${D}`,
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
console.log(`music starts at ${P.MUSIC_START} s of the track, ${P.BPM} BPM, cut to ${D} s`);

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
