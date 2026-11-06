// 把 record.js 录好的各段拼成成片：配乐混好，编两个版本，写一份章节时间。
//   out/MineDeck-demo-1440p60.mp4   2560×1440（主版本）
//   out/MineDeck-demo-1080p60.mp4   1920×1080
//   out/章节.txt                    发 B 站时贴进简介
// 配乐是作者按 配乐提示词.md 生成的四段，同调、同速。每段比片子需要的长，所以各截一段，卡在小节线上交叉淡化，
// 听起来像一首曲子。音高和速度都不动。
// 视频编码的参数和 tools/promo/video/encode.js 一样：High、yuv420p、CRF 16、BT.709、恒定 60 fps、+faststart。
// 用法：node tools/promo/tutorial/assemble.js
"use strict";
const path = require("path");
const fs = require("fs");
const { spawnSync, execFileSync } = require("child_process");

const HERE = __dirname, OUT = path.join(HERE, "out"), SEG = path.join(OUT, "seg");
const MUSIC_DIR = path.join(HERE, "music");
// 节拍网格是量出来的：前三段都是 101.98 BPM，第 0 拍是强拍（底鼓在这上面），两小节一个循环。beat(k) = 第 k 拍的时刻。
// 截点都取 8 的倍数（两小节），交叉淡化也是 8 拍，所以重叠的那段鼓点是对齐的。
const BEAT = 0.58835, PHASE = 0.039, beat = k => PHASE + k * BEAT;
const X = 8 * BEAT;
// 第 1 段从头开始：前 9 秒是轻前奏，正好垫开场，律动进来时切到第一章。104 秒起的安静段不用。
// 第 2 段从 80 秒开始，避开 78 秒那一下硬停顿。第 3 段从 42 秒开始，前面的编配偏轻，40 秒还有一处停顿。
// 第 4 段是 104 BPM，和前面差 2%：让它的第 88 拍落在交叉淡化正中，重叠 4.7 秒里前后最多差 40 毫秒。to 不写就是用到曲子结尾。
const TRACKS = [
  { f: "demo-1.mp3", from: 0, to: beat(168) },
  { f: "demo-2.mp3", from: beat(136), to: beat(288) },
  { f: "demo-3.mp3", from: beat(72), to: beat(240) },
  { f: "demo-4.mp3", from: 0.106 + 88 * 0.57699 - X / 2 },
];
// -16 LUFS：这几段的真峰值都接近或超过 0 dBTP，调到 -14 会被 -1.5 dBTP 的上限卡住，每段卡的量不同，响度就不一样了
const TARGET_I = -16, CEIL_TP = -1.5, FPS = 60;
const MUSIC_ONLY = process.argv.includes("--music-only"); // 只换音轨：视频流原样拷贝，不重新编码
const WAV = path.join(OUT, "music-demo-48k.wav");
const VF = "scale=out_color_matrix=bt709:out_range=tv,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv";
const VF_1080 = "scale=1920:1080:flags=lanczos+accurate_rnd+full_chroma_int:out_color_matrix=bt709:out_range=tv,format=yuv420p,setparams=colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv";
const COMMON = ["-c:v", "libx264", "-profile:v", "high", "-preset", "slow", "-crf", "16", "-g", "120", "-pix_fmt", "yuv420p",
  "-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv", "-r", "60", "-fps_mode", "cfr", "-movflags", "+faststart"];

function run(args) {
  const r = spawnSync("ffmpeg", ["-hide_banner", "-loglevel", "warning", "-stats", "-y", ...args], { stdio: "inherit" });
  if (r.status !== 0) throw new Error("ffmpeg exited with " + r.status);
}
function loud(file, pre = "") {
  const r = spawnSync("ffmpeg", ["-hide_banner", "-nostats", "-i", file, "-af", (pre ? pre + "," : "") + "loudnorm=print_format=json", "-f", "null", "-"], { encoding: "utf8" });
  const j = JSON.parse(r.stderr.match(/\{[^{}]*"input_i"[^{}]*\}/)[0]);
  return { I: +j.input_i, TP: +j.input_tp };
}

// ---------- 画面 ----------
const ids = require("./scenes.js").map(s => s.id);
const segs = ids.map(id => JSON.parse(fs.readFileSync(path.join(SEG, id + ".json"), "utf8")));
const list = path.join(OUT, "concat.txt");
fs.writeFileSync(list, ids.map(id => `file '${path.join(SEG, id + ".mkv").split(path.sep).join("/")}'`).join("\n") + "\n");
const frames = segs.reduce((a, s) => a + s.frames, 0), D = frames / FPS;

const pad2 = n => String(n).padStart(2, "0");
const stamp = s => `${pad2(Math.floor(s / 60))}:${pad2(Math.floor(s % 60))}`;
let at = 0;
const chapters = segs.map(s => { const l = `${stamp(at / FPS)} ${s.title}`; at += s.frames; return l; });
fs.writeFileSync(path.join(OUT, "章节.txt"), chapters.join("\n") + "\n");
console.log(`${frames} frames, ${D.toFixed(2)} s`);

// ---------- 配乐 ----------
const info = TRACKS.map(t => {
  const file = path.join(MUSIC_DIR, t.f);
  const dur = +execFileSync("ffprobe", ["-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", file], { encoding: "utf8" });
  const to = t.to ?? dur, trim = `atrim=start=${t.from.toFixed(4)}:end=${to.toFixed(4)},asetpts=PTS-STARTPTS`;
  const l = loud(file, trim); // 响度按用到的这一段量
  const gain = Math.min(TARGET_I - l.I, CEIL_TP - l.TP);
  console.log(`${t.f}: ${t.from.toFixed(2)}-${to.toFixed(2)} s, ${l.I} LUFS, ${l.TP} dBTP, gain ${gain.toFixed(2)} dB`);
  return { file, trim, gain, len: to - t.from };
});
const sum = info.reduce((a, t) => a + t.len, 0) - X * (info.length - 1);
console.log(`music ${sum.toFixed(1)} s, video ${D.toFixed(1)} s, crossfade ${X.toFixed(2)} s`);
// 音乐要盖住片子。多出来的部分被末尾的淡出截掉；不够时片尾会静一段，差太多就报错，要改上面的截点
if (sum < D - 3) throw new Error(`music too short: ${sum.toFixed(1)} s for ${D.toFixed(1)} s, adjust TRACKS`);
let fc = info.map((t, i) => `[${i}:a]${t.trim},aresample=48000,aformat=channel_layouts=stereo,volume=${t.gain.toFixed(3)}dB[t${i}]`).join(";");
let prev = "t0";
for (let i = 1; i < info.length; i++) { fc += `;[${prev}][t${i}]acrossfade=d=${X.toFixed(4)}:c1=qsin:c2=qsin[m${i}]`; prev = "m" + i; }
fc += `;[${prev}]afade=t=in:d=0.6,afade=t=out:st=${(D - 2.5).toFixed(3)}:d=2.5,apad=whole_dur=${D.toFixed(4)},atrim=duration=${D.toFixed(4)},asetpts=PTS-STARTPTS[a]`;
const raw = path.join(OUT, "music-demo-raw.wav");
run([...info.flatMap(t => ["-i", t.file]), "-filter_complex", fc, "-map", "[a]", "-c:a", "pcm_s24le", raw]);
const mix = loud(raw), g = Math.min(TARGET_I - mix.I, CEIL_TP - mix.TP);
console.log(`mix ${mix.I} LUFS, ${mix.TP} dBTP, gain ${g.toFixed(2)} dB`);
run(["-i", raw, "-af", `volume=${g.toFixed(3)}dB`, "-c:a", "pcm_s24le", WAV]);
fs.rmSync(raw, { force: true });

// ---------- 编码 ----------
const report = [];
for (const j of [{ file: "MineDeck-demo-1440p60.mp4", w: 2560, h: 1440, vf: VF }, { file: "MineDeck-demo-1080p60.mp4", w: 1920, h: 1080, vf: VF_1080 }]) {
  const out = path.join(OUT, j.file), audio = ["-c:a", "aac", "-b:a", "256k", "-ar", "48000", "-ac", "2"];
  if (MUSIC_ONLY) {
    const tmp = out + ".tmp.mp4";
    run(["-i", out, "-i", WAV, "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy", ...audio, "-movflags", "+faststart", tmp]);
    fs.renameSync(tmp, out);
  } else {
    run(["-f", "concat", "-safe", "0", "-i", list, "-i", WAV, "-map", "0:v:0", "-map", "1:a:0", "-vf", j.vf, ...COMMON, ...audio, "-shortest", out]);
  }
  const p = JSON.parse(execFileSync("ffprobe", ["-v", "error", "-count_frames", "-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,nb_read_frames,sample_rate,channels:format=duration,size", "-of", "json", out], { encoding: "utf8" }));
  const v = p.streams.find(s => s.codec_type === "video"), a = p.streams.find(s => s.codec_type === "audio");
  const ok = v.width === j.w && v.height === j.h && v.r_frame_rate === "60/1" && +v.nb_read_frames === frames && a && a.codec_name === "aac" && Math.abs(+p.format.duration - D) < 0.1;
  const line = `${j.file}: ${v.width}x${v.height} ${v.r_frame_rate} fps, ${v.nb_read_frames} frames, ${(+p.format.duration).toFixed(2)} s, ${(+p.format.size / 1048576).toFixed(1)} MB, audio ${a && a.codec_name} ${a && a.sample_rate} Hz`;
  console.log(line + (ok ? "  OK" : "  FAILED"));
  report.push(line);
  if (!ok) process.exit(1);
}
fs.writeFileSync(path.join(OUT, "ffprobe.txt"), report.join("\n") + "\n" + chapters.join("\n") + "\n");
// 联系表：每 8 秒一帧，看片用
run(["-i", path.join(OUT, "MineDeck-demo-1440p60.mp4"), "-vf", "select='not(mod(n\\,480))',scale=384:-1:flags=lanczos,tile=5x8:padding=4:color=white", "-fps_mode", "vfr", "-frames:v", "1", "-update", "1", path.join(OUT, "contact-sheet.png")]);
console.log("done");
