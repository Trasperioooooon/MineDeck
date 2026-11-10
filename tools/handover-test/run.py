# -*- coding: utf-8 -*-
"""把一条测试放到后台去跑，再回来看进度。测试一跑就是几分钟到几十分钟，很多智能体平台对单条命令有时间限制，
所以所有测试都这样启动：

    python run.py handover.py --id F-1a --core forge ……     启动（马上返回）
    python run.py --status                                  看进度：还在不在跑、最近打印了什么

一次只能跑一条：上一条没结束时不会启动下一条。输出同时存在 results/<编号>/输出.txt。
"""
import io
import json
import os
import subprocess
import sys
import time

for _s in (sys.stdout, sys.stderr):
    try:
        _s.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

ROOT = os.path.dirname(os.path.abspath(__file__))
STATE = os.path.join(ROOT, "work", "current-run.json")
ALLOWED = ("handover.py", "cfpack.py")


def alive(pid):
    try:
        out = subprocess.run(["tasklist", "/FI", "PID eq %d" % int(pid), "/FO", "CSV", "/NH"],
                             capture_output=True, timeout=20).stdout.decode("mbcs", "replace")
    except Exception:
        return False
    return ('"%d"' % int(pid)) in out and "python" in out.lower()


def load():
    try:
        with io.open(STATE, encoding="utf-8") as f:
            return json.load(f)
    except Exception:
        return None


def status():
    st = load()
    if not st:
        print("还没有启动过测试。")
        return 0
    running = alive(st["pid"])
    mins = (time.time() - st["started"]) / 60.0
    print("测试 %s：%s（启动于 %.0f 分钟前）" % (st["id"], "还在运行" if running else "已结束", mins))
    try:
        with open(st["out"], "rb") as f:
            lines = f.read().decode("utf-8", "replace").splitlines()
    except OSError:
        lines = []
    print("—— 输出的最后 %d 行（全部在 %s）——" % (min(len(lines), 25), os.path.relpath(st["out"], ROOT)))
    for ln in lines[-25:]:
        print(ln)
    if not running and not any(ln.startswith("总结果：") for ln in lines):
        print("（进程已经不在了，但输出里没有「总结果」那一行：它是被中途打断的，这一项要重跑或者记「没法测」）")
    return 0


def start(argv):
    script = argv[0]
    if os.path.basename(script) not in ALLOWED or os.path.dirname(script) not in ("", ".", ROOT):
        print("只能启动测试目录里的 %s" % " / ".join(ALLOWED))
        return 2
    if "--id" not in argv or argv.index("--id") + 1 >= len(argv):
        print("命令里要有 --id 编号")
        return 2
    test_id = argv[argv.index("--id") + 1]
    st = load()
    if st and alive(st["pid"]):
        print("测试 %s 还在运行，等它结束再启动下一条（python run.py --status 看进度）。" % st["id"])
        return 1
    folder = os.path.join(ROOT, "results", test_id)
    os.makedirs(folder, exist_ok=True)
    os.makedirs(os.path.dirname(STATE), exist_ok=True)
    out = os.path.join(folder, "输出.txt")
    cmd = [sys.executable, "-u", os.path.join(ROOT, os.path.basename(script))] + argv[1:]
    # 不开窗口、脱离当前终端：平台结束这条命令以后测试还能接着跑
    flags = 0x08000000 | 0x00000200  # CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP
    with open(out, "wb") as f:
        try:
            p = subprocess.Popen(cmd, cwd=ROOT, stdin=subprocess.DEVNULL, stdout=f, stderr=subprocess.STDOUT,
                                 creationflags=flags | 0x01000000)  # CREATE_BREAKAWAY_FROM_JOB
        except OSError:
            p = subprocess.Popen(cmd, cwd=ROOT, stdin=subprocess.DEVNULL, stdout=f, stderr=subprocess.STDOUT, creationflags=flags)
    with io.open(STATE, "w", encoding="utf-8") as f:
        json.dump({"id": test_id, "pid": p.pid, "out": out, "started": time.time(), "cmd": argv}, f, ensure_ascii=False)
    print("测试 %s 已在后台开始。" % test_id)
    print("看进度：python run.py --status    结束的标志是最后一行「总结果：……」")
    return 0


if __name__ == "__main__":
    if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help"):
        print(__doc__)
        sys.exit(0)
    if sys.argv[1] == "--status":
        sys.exit(status())
    sys.exit(start(sys.argv[1:]))
