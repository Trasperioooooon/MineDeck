"""把 assets/icon.ico 里 256 像素那一帧（32×32 的像素画，每格 8 像素）按整数倍放大成封面用的透明 PNG。
用法（在仓库根目录）：python -I tools/promo/video-v3.1/export_icon.py assets/icon.ico tools/promo/video-v3.1/out/封面素材
需要 Pillow。"""
import os
import sys

from PIL import Image

src, outdir = sys.argv[1], sys.argv[2]
ico = Image.open(src)
sizes = sorted(ico.info.get("sizes") or [ico.size])
big = max(sizes)
if big != ico.size:
    ico.size = big
im = ico.convert("RGBA")
if im.size != (256, 256):
    raise SystemExit(f"最大一帧是 {im.size}，不是 256×256")

CELL, GRID = 8, 32
small = Image.new("RGBA", (GRID, GRID))
px, sp = im.load(), small.load()
bad = 0
for y in range(GRID):
    for x in range(GRID):
        c = px[x * CELL + CELL // 2, y * CELL + CELL // 2]
        for dy in range(CELL):
            for dx in range(CELL):
                if px[x * CELL + dx, y * CELL + dy] != c:
                    bad += 1
        sp[x, y] = c
if bad:
    raise SystemExit(f"有 {bad} 个像素和所在格子的中心颜色不同，不是整齐的 8 像素网格")

alphas = sorted({sp[x, y][3] for x in range(GRID) for y in range(GRID)})
os.makedirs(outdir, exist_ok=True)
for n in (1024, 2048):
    out = os.path.join(outdir, f"minedeck-icon-{n}.png")
    small.resize((n, n), Image.NEAREST).save(out, optimize=True)
    print(out, os.path.getsize(out), "bytes")
print("alpha values:", alphas)
