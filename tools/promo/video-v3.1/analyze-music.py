"""量配乐：速度、拍点、小节线、每小节的响度和高低频能量，排画面时照着它把段落对到曲子上。

用法（仓库根目录）：
  python -I tools/promo/video-v3.1/analyze-music.py tools/promo/video-v3.1/music/<曲子>.mp3 [--png 图片路径] [--json 结果路径] [--downbeat-phase 0-3]
需要 ffmpeg（在 PATH 里）、numpy、scipy。

做法：
  1. ffmpeg 解成 22050 Hz 单声道；短时傅里叶变换（窗 2048、跳 256，约 11.6 毫秒一格）。
  2. 对数幅度谱的正向差分求和得到起音强度曲线（spectral flux），减掉 0.5 秒的滑动平均。
  3. 速度：起音曲线的自相关在 60–200 BPM 里找峰（按 120 BPM 附近加一点先验，免得取到一半或两倍），
     再在 ±2 BPM、全部相位上做「梳子」搜索，取拍点上起音强度平均值最大的固定速度网格。
  4. 速度稳不稳：每 16 拍一段，看这一段自己的最佳相位离全局网格差多少毫秒。
  5. 小节的第一拍（拍号从第一个拍点数起，按 mod 4 分四种相位）：
     a. 和弦变化：每拍的色度（110–2000 Hz 按半音折成 12 个音级，另算一张窗 4096 的谱）和前一拍比，按相位平均。
        这类曲子和弦在每小节第 1 拍换，常常第 3 拍也换，所以得分高的是第 1 拍或第 3 拍，差两拍分不开。
     b. 段落的起点：每拍往后 4 拍和往前 4 拍的响度差（dB），涨 3 dB 以上的峰（鼓进来、主段落落下）几乎都落在小节第一拍上。
     在 a 得分最高的一对相位（p 和 p+2）里，取 b 的峰落得多的那个。
     旧做法（低频起音落在拍上的平均强度最大的相位）也照样打出来做参考：Morning on the Balcony 上它差了一拍，
     落在第 4 拍，所以改成上面这样。拿不准时用 --downbeat-phase 指定，再对着谱图听一遍。
  6. 每小节的响度（RMS dB）、低中高三段能量、高频起音密度，用来认前奏、律动进来、蓄力、主段落、结尾。
"""
import argparse
import json
import subprocess
import sys

import numpy as np
from scipy.ndimage import gaussian_filter1d, uniform_filter1d

SR, NFFT, HOP = 22050, 2048, 256


def decode(path):
    out = subprocess.run(["ffmpeg", "-v", "error", "-i", path, "-ac", "1", "-ar", str(SR), "-f", "f32le", "-"],
                         capture_output=True, check=True).stdout
    return np.frombuffer(out, dtype="<f4").astype(np.float64)


def spectrogram(x, nfft=NFFT):
    pad = nfft // 2
    xp = np.pad(x, (pad, pad))
    n = 1 + (len(xp) - nfft) // HOP
    win = np.hanning(nfft).astype(np.float32)
    S = np.empty((n, nfft // 2 + 1), dtype=np.float32)
    for a in range(0, n, 512):
        b = min(n, a + 512)
        idx = np.arange(nfft)[None, :] + HOP * np.arange(a, b)[:, None]
        S[a:b] = np.abs(np.fft.rfft(xp[idx].astype(np.float32) * win, axis=1))
    return S


def chord_change(x, beats, fps):
    """每拍的色度和前一拍差多少（0 = 一样，1 = 完全不同）；第 i 个值是第 i 拍。"""
    S = spectrogram(x, 4096)
    fq = np.fft.rfftfreq(4096, 1 / SR)
    m = (fq >= 110) & (fq <= 2000)
    pc = (np.round(12 * np.log2(fq[m] / 440.0)) % 12).astype(int)
    P = S[:, m].astype(np.float64) ** 2
    C = np.stack([P[:, pc == k].sum(1) for k in range(12)], 1)
    C = np.log1p(C / (C.max() * 1e-4))
    v = []
    for i in range(len(beats) - 1):
        a_ = int(beats[i] * fps)
        c = C[a_:max(int(beats[i + 1] * fps), a_ + 1)].mean(0)
        v.append(c / (np.linalg.norm(c) + 1e-9))
    v = np.array(v)
    return np.r_[0, 1 - (v[1:] * v[:-1]).sum(1)]


def section_jumps(x, beats, min_db=3.0):
    """段落起点：每拍往后 4 拍比往前 4 拍（跳过紧挨着的一拍）响多少 dB，取 ±3 拍里最大、又涨了 min_db 以上的峰，返回 [(拍号, dB)]。"""
    nb = len(beats) - 1
    bdb = np.array([20 * np.log10(np.sqrt((x[int(beats[i] * SR):int(beats[i + 1] * SR)] ** 2).mean()) + 1e-12) for i in range(nb)])
    jump = np.zeros(nb)
    for i in range(5, nb - 4):
        jump[i] = bdb[i:i + 4].mean() - bdb[i - 5:i - 1].mean()  # 往前那 4 拍跳过紧挨着的一拍：段落前常有一拍加花或蓄力，别让峰提前一拍
    return [(i, round(float(jump[i]), 1)) for i in range(nb) if jump[i] >= min_db and jump[i] == jump[max(0, i - 3):i + 4].max()]


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("mp3")
    ap.add_argument("--png")
    ap.add_argument("--json")
    ap.add_argument("--prior", type=float, default=120.0, help="速度先验（BPM）")
    ap.add_argument("--downbeat-phase", type=int, choices=range(4), help="不用自动判断，直接指定小节第一拍的相位（0–3）")
    a = ap.parse_args()

    x = decode(a.mp3)
    dur = len(x) / SR
    S = spectrogram(x)
    n = S.shape[0]
    fps = SR / HOP
    times = np.arange(n) / fps
    freqs = np.fft.rfftfreq(NFFT, 1 / SR)

    L = np.log1p(100.0 * S)
    flux = np.zeros(n)
    flux[1:] = np.maximum(0, L[1:] - L[:-1]).sum(axis=1)
    band = lambda lo, hi: (freqs >= lo) & (freqs < hi)
    low_m, mid_m, high_m = band(30, 150), band(150, 2000), band(2000, 9000)
    low_flux = np.zeros(n); low_flux[1:] = np.maximum(0, L[1:, low_m] - L[:-1, low_m]).sum(axis=1)
    high_flux = np.zeros(n); high_flux[1:] = np.maximum(0, L[1:, high_m] - L[:-1, high_m]).sum(axis=1)

    def novelty(f):
        g = f - uniform_filter1d(f, size=int(0.5 * fps))
        g = np.maximum(g, 0)
        return g / (g.max() or 1)

    onset, low_on, high_on = novelty(flux), novelty(low_flux), novelty(high_flux)
    env = gaussian_filter1d(onset, 1.5)

    # 速度：自相关 + 先验
    o = onset - onset.mean()
    ac = np.correlate(o, o, "full")[n - 1:]
    bpms = np.arange(60.0, 200.0, 0.25)
    lags = 60.0 / bpms * fps
    acv = np.interp(lags, np.arange(len(ac)), ac)
    prior = np.exp(-0.5 * (np.log2(bpms / a.prior) / 0.6) ** 2)
    est = bpms[np.argmax(acv * prior)]

    # 梳子搜索：固定速度网格
    best = (-1, 0, 0)
    for bpm in np.arange(est - 2, est + 2, 0.005):
        per = 60.0 / bpm
        phases = np.arange(0, per, 0.004)
        k = np.arange(int(dur / per) + 1)
        tt = phases[:, None] + per * k[None, :]
        ok = tt < dur - 0.05
        vals = np.interp(tt * fps, np.arange(n), env) * ok
        score = vals.sum(axis=1) / ok.sum(axis=1)
        i = int(np.argmax(score))
        if score[i] > best[0]:
            best = (score[i], bpm, phases[i])
    _, bpm, phase = best
    per = 60.0 / bpm
    beats = np.arange(phase, dur, per)

    # 速度稳不稳：每 16 拍一段的最佳相位偏差
    drift = []
    for s in range(0, len(beats) - 16, 16):
        offs = np.arange(-0.06, 0.0601, 0.002)
        bt = beats[s:s + 16]
        sc = [np.interp((bt + d) * fps, np.arange(n), env).mean() for d in offs]
        drift.append((round(float(bt[0]), 2), round(float(offs[int(np.argmax(sc))]) * 1000)))

    # 小节第一拍：四种相位。旧做法（起音强度）只做参考，用和弦变化定一对候选，段落起点定是哪一个
    low_s, all_s = gaussian_filter1d(low_on, 1.5), env
    at = lambda f, tt: np.interp(tt * fps, np.arange(n), f)
    ph_scores = []
    for p in range(4):
        bt = beats[p::4]
        ph_scores.append((p, round(float(at(low_s, bt).mean()), 4), round(float(at(all_s, bt).mean()), 4)))
    onset_down = max(ph_scores, key=lambda r: r[1] + 0.5 * r[2])[0]
    cc = chord_change(x, beats, fps)
    chord_ph = [round(float(cc[p::4].mean()), 4) for p in range(4)]
    jumps = section_jumps(x, beats)
    jump_ph = [round(sum(j for i, j in jumps if i % 4 == p), 1) for p in range(4)]
    pair = max(range(2), key=lambda p: chord_ph[p] + chord_ph[p + 2])
    if a.downbeat_phase is not None:
        down, how = a.downbeat_phase, "given by --downbeat-phase"
    elif jump_ph[pair] != jump_ph[pair + 2]:
        down, how = (pair if jump_ph[pair] > jump_ph[pair + 2] else pair + 2), "chord changes + section starts"
    else:
        down, how = (pair if chord_ph[pair] >= chord_ph[pair + 2] else pair + 2), "chord changes only (no clear section start: check by ear)"
    bar0 = beats[down] - 4 * per * np.ceil(beats[down] / (4 * per) - 1e-9)  # 第一个小节线（可能是负数）
    bars = np.arange(bar0, dur + 1e-9, 4 * per)
    bar1 = bar0 if bar0 >= 0 else bar0 + 4 * per  # 第 1 小节的起点（第一条不是负数的小节线）

    # 每小节的统计
    P = S.astype(np.float64) ** 2
    rms = np.sqrt(uniform_filter1d(x ** 2, size=HOP)[::HOP][:n] + 1e-12)
    if len(rms) < n:
        rms = np.pad(rms, (0, n - len(rms)), mode="edge")
    eL, eM, eH = P[:, low_m].sum(1), P[:, mid_m].sum(1), P[:, high_m].sum(1)
    db = lambda v: 10 * np.log10(v + 1e-12)
    rows = []
    for i in range(len(bars) - 1):
        t0, t1 = bars[i], bars[i + 1]
        if t1 <= 0 or t0 >= dur:
            continue
        m = (times >= max(t0, 0)) & (times < min(t1, dur))
        if not m.any():
            continue
        rows.append({
            "bar": i + 1 if bar0 >= 0 else i, "t0": round(float(t0), 3),
            "rms_db": round(float(20 * np.log10(np.sqrt((rms[m] ** 2).mean()) + 1e-12)), 1),
            "low_db": round(float(db(eL[m].mean())), 1), "mid_db": round(float(db(eM[m].mean())), 1),
            "high_db": round(float(db(eH[m].mean())), 1),
            "kick": round(float(at(low_s, beats[(beats >= t0) & (beats < t1)]).mean() if ((beats >= t0) & (beats < t1)).any() else 0), 3),
            "hats": round(float(high_on[m].mean()), 3),
        })

    res = {"file": a.mp3, "duration": round(dur, 3), "bpm_autocorr": float(est), "bpm": round(float(bpm), 3),
           "beat_period": round(per, 5), "first_beat": round(float(phase), 4), "downbeat_phase": int(down), "downbeat_by": how,
           "first_bar_line": round(float(bar0), 4), "bar1_start": round(float(bar1), 4),
           "chord_change_by_phase": chord_ph, "section_starts(beat,dB)": jumps, "section_start_dB_by_phase": jump_ph,
           "onset_phase_scores(low,all)": ph_scores, "onset_downbeat_phase": int(onset_down), "drift_ms_per_16_beats": drift,
           "bars": rows}

    print(f"duration {dur:.3f} s, tempo {bpm:.3f} BPM (autocorr {est}), beat {per*1000:.2f} ms, bar {4*per:.4f} s")
    print(f"first beat at {phase:.4f} s; phase = beat number from it, mod 4")
    print(f"  chord change by phase: {chord_ph}")
    print(f"  section starts (beat, +dB): {jumps} -> dB by phase {jump_ph}")
    print(f"  onset strength by phase (phase, low, all), reference only: {ph_scores} -> would pick {onset_down}")
    print(f"downbeat phase {down} ({how}); bar 1 starts at {bar1:.4f} s, first bar line {bar0:.4f} s")
    print("drift per 16 beats (start s, best offset ms):", drift)
    print(f"{'bar':>4} {'t0':>8} {'rms':>6} {'low':>6} {'mid':>6} {'high':>6} {'kick':>6} {'hats':>6}")
    for r in rows:
        print(f"{r['bar']:>4} {r['t0']:>8.3f} {r['rms_db']:>6.1f} {r['low_db']:>6.1f} {r['mid_db']:>6.1f} {r['high_db']:>6.1f} {r['kick']:>6.3f} {r['hats']:>6.3f}")

    if a.json:
        with open(a.json, "w", encoding="utf-8") as f:
            json.dump(res, f, ensure_ascii=False, indent=1)

    if a.png:
        from PIL import Image, ImageDraw
        pxs = 30  # 每秒像素
        Wd, Hs, Hc = int(dur * pxs) + 80, 420, 260
        img = Image.new("RGB", (Wd, Hs + Hc + 40), (250, 248, 240))
        # 对数频率的谱图（60 Hz – 9 kHz）
        fl = np.geomspace(60, 9000, Hs)
        fi = np.clip(np.searchsorted(freqs, fl), 0, len(freqs) - 1)
        col = np.clip(np.interp(np.arange(Wd - 80) / pxs * fps, np.arange(n), np.arange(n)).astype(int), 0, n - 1)
        spec = L[col][:, fi].T[::-1]
        spec = (spec - spec.min()) / (np.ptp(spec) or 1)
        rgb = np.stack([255 - 200 * spec, 255 - 120 * spec, 255 - 210 * spec], -1).astype(np.uint8)
        img.paste(Image.fromarray(rgb), (40, 0))
        d = ImageDraw.Draw(img)
        # 曲线：响度和三段能量
        tt = np.arange(Wd - 80) / pxs
        def curve(v, color, lo, hi):
            vv = np.interp(tt * fps, np.arange(n), gaussian_filter1d(v, 10))
            ys = Hs + 20 + (Hc - 20) * (1 - np.clip((vv - lo) / (hi - lo), 0, 1))
            d.line(list(zip(40 + np.arange(len(tt)), ys)), fill=color, width=2)
        curve(20 * np.log10(rms + 1e-9), (30, 30, 30), -50, -5)
        curve(db(eL), (200, 60, 40), np.percentile(db(eL), 5), db(eL).max())
        curve(db(eH), (40, 90, 200), np.percentile(db(eH), 5), db(eH).max())
        for b in beats:
            X = 40 + b * pxs
            d.line([(X, Hs), (X, Hs + 8)], fill=(150, 150, 150))
        for i, b in enumerate(bars):
            if b < 0:
                continue
            X = 40 + b * pxs
            d.line([(X, 0), (X, Hs + Hc)], fill=(60, 140, 40) if i % 4 == 0 else (190, 200, 180), width=1)
            d.text((X + 2, Hs + Hc + 4), f"{i + 1 if bar0 >= 0 else i}", fill=(40, 40, 40))
        for s in range(0, int(dur) + 1, 5):
            d.text((40 + s * pxs, Hs + Hc + 22), f"{s}s", fill=(120, 60, 60))
        img.save(a.png)
        print("wrote", a.png)


if __name__ == "__main__":
    sys.exit(main())
