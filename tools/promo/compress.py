#!/usr/bin/env python3
"""压缩 PNG 截图：先试无损，还超过上限才降到 256 色调色板。

用法：python3 -I compress.py <上限字节数> <输入.png> <输出.png> [<输入.png> <输出.png> ...]

每一对都先试几种无损重新编码（Pillow 的 zlib 9 级；PATH 里有 ImageMagick 的 convert 时再加它的自适应过滤），留最小的。
还大于 <上限字节数> 才退回 256 色调色板（Pillow 中位切分 + Floyd-Steinberg 抖动）。
每个文件输出一行 JSON：{"out", "w", "h", "raw", "bytes", "method"}。
"""
import io
import json
import shutil
import subprocess
import sys
import tempfile

from PIL import Image


def lossless_candidates(src_path, im):
    out = []
    buf = io.BytesIO()
    im.save(buf, "PNG", optimize=True, compress_level=9)
    out.append(("pillow-lossless", buf.getvalue()))
    if shutil.which("convert"):
        for strategy in ("1", "0"):
            with tempfile.NamedTemporaryFile(suffix=".png") as tmp:
                cmd = ["convert", src_path, "-strip", "-define", "png:compression-level=9",
                       "-define", "png:compression-filter=5", "-define", f"png:compression-strategy={strategy}",
                       "PNG24:" + tmp.name if im.mode == "RGB" else "PNG32:" + tmp.name]
                if subprocess.run(cmd, capture_output=True).returncode == 0:
                    with open(tmp.name, "rb") as f:
                        out.append((f"imagemagick-lossless-s{strategy}", f.read()))
    return out


def main():
    limit = int(sys.argv[1])
    pairs = sys.argv[2:]
    if not pairs or len(pairs) % 2:
        sys.exit(__doc__)
    for src, dst in zip(pairs[0::2], pairs[1::2]):
        with open(src, "rb") as f:
            raw = f.read()
        im = Image.open(io.BytesIO(raw))
        im.load()
        if im.mode == "RGBA" and im.getextrema()[3][0] == 255:
            im = im.convert("RGB")  # 完全不透明：去掉 alpha 通道
        cands = [("original", raw)] + lossless_candidates(src, im)
        method, data = min(cands, key=lambda c: len(c[1]))
        if len(data) > limit:
            q = im.convert("RGB").quantize(colors=256, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.FLOYDSTEINBERG)
            buf = io.BytesIO()
            q.save(buf, "PNG", optimize=True)
            method, data = "pillow-palette256", buf.getvalue()
        with open(dst, "wb") as f:
            f.write(data)
        print(json.dumps({"out": dst, "w": im.width, "h": im.height, "raw": len(raw), "bytes": len(data), "method": method}))


if __name__ == "__main__":
    main()
