# -*- coding: utf-8 -*-
"""交接包的一条完整流程：副本 A 上备好一台服务器 → 导出交接包 → 传给副本 B → 导入 → 开服 → 逐个文件比对。

每一步的检查会打印 [通过] / [不通过] / [信息]，原始证据存进 results/<编号>/，最后一行是总结果。
退出码：0 全部通过，1 有不通过，2 流程走不下去（中途停了）。

例子（在测试目录里运行）：

    python handover.py --id F-1a --core forge --mc 1.20.1 --src fg-src --src-port 25741 --dst fg-slim --dst-port 25751 --mod jei
    python handover.py --id F-1b --core forge --mc 1.20.1 --src fg-src --src-port 25741 --dst fg-full --dst-port 25752 --full --mode distributable
    python handover.py --id G-1  --core paper --mc 1.20.1 --src big-src --src-port 25744 --dst big-many --dst-port 25757 --filler 5x1024
    python handover.py --id G-3  --core paper --mc 1.20.1 --src big-src --src-port 25744 --dst big-over --dst-port 25759 --filler 9x1024 --expect-upload-reject

源服务器已经在就直接用（不会重建），所以同一台源服务器可以导出很多次。目标服务器的名字每次要换一个新的。
"""
import argparse
import glob
import os
import shutil
import sys
import time

from mdtest import A, B, WORK, MemWatch, Timer, make_filler, q, read_text, save, sha256_file

checks = []  # (结果, 说明)


def note(ok, text):
    """ok: True 通过 / False 不通过 / None 只是信息。"""
    tag = "[信息]" if ok is None else ("[通过]" if ok else "[不通过]")
    checks.append((tag, text))
    print("%s %s" % (tag, text), flush=True)


def finish(args, aborted=""):
    failed = [c for c in checks if c[0] == "[不通过]"]
    lines = ["%s %s" % c for c in checks]
    if aborted:
        verdict = "中途停了：" + aborted
    elif failed:
        verdict = "有 %d 项不通过" % len(failed)
    else:
        verdict = "全部通过"
    lines.append("总结果：" + verdict)
    save(args.id, "检查结果.txt", "\n".join(lines) + "\n")
    print("总结果：%s（证据在 results\\%s\\）" % (verdict, args.id), flush=True)
    sys.exit(2 if aborted else (1 if failed else 0))


def tree(folder, skip_prefixes=()):
    """{相对路径: SHA-256}。session.lock 和 MineDeck 自己的文件不算（它们本来就不进包）。"""
    out = {}
    for base, _dirs, files in os.walk(folder):
        for name in files:
            p = os.path.join(base, name)
            rel = os.path.relpath(p, folder).replace("\\", "/")
            if name == "session.lock" or any(rel.startswith(s) for s in skip_prefixes):
                continue
            out[rel] = sha256_file(p)
    return out


def temp_multipart():
    """系统临时目录里 Go 存上传内容的临时文件：{路径: 字节数}。只看不动。"""
    out = {}
    for p in glob.glob(os.path.join(os.environ.get("TEMP", ""), "multipart-*")):
        try:
            out[p] = os.path.getsize(p)
        except OSError:
            pass
    return out


def mb(n):
    return "%.1f MB" % (n / 1048576.0)


def mem_check(label, mem):
    """内存有没有跟着包的大小涨。一次读数都没采到的话不能算通过：记成信息，让测试员知道这一项没量到。"""
    if mem.peak_mb < 0:
        note(None, "%s 的内存没有采到读数，这一项没量到" % label)
    else:
        note(mem.peak_mb < 1024, "%s 的内存最高 %.0f MB（预期不随包的大小上涨，应在 1 GB 以内）" % (label, mem.peak_mb))


def task_ok(task):
    return bool(task.get("ended")) and not task.get("error") and not task.get("timeout")


def boot(copy, name, test_id, label, limit):
    """开服 → 等日志里出现 Done → 停服。返回 (开起来了没有, 用了几秒)。"""
    started, why = copy.start_checked(name)
    if not started:
        note(False, "%s：%s" % (label, why))
        return False, 0
    if why:
        note(None, "%s：%s" % (label, why))
    ok, secs, line = copy.wait_log(name, limit=limit)
    log_path = os.path.join(copy.inst_dir(name), "logs", "latest.log")
    save(test_id, label + "-latest.log末尾.txt", read_text(log_path, tail=120))
    note(ok, "%s：%s（%.0f 秒）%s" % (label, "开服完成" if ok else "没有开起来", secs, line[:140]))
    copy.stop(name)
    stopped = copy.wait_stopped(name, limit=240)
    note(True if stopped else False, "%s：停服%s" % (label, "完成" if stopped else "超过 4 分钟还没停下来"))
    return ok, secs


def main():
    ap = argparse.ArgumentParser(description="交接包的一条完整流程（A 导出 → B 导入 → 开服 → 比对）")
    ap.add_argument("--id", required=True, help="测试编号，证据存进 results/<编号>/")
    ap.add_argument("--core", required=True, help="核心：forge / neoforge / fabric / paper / vanilla ……")
    ap.add_argument("--mc", required=True, help="Minecraft 版本，如 1.20.1")
    ap.add_argument("--build", default="", help="核心的构建号；不写就用接口标了 recommended 的，没有就用第一个")
    ap.add_argument("--src", required=True, help="副本 A 上源服务器的名字（已经在就直接用）")
    ap.add_argument("--src-port", type=int, required=True)
    ap.add_argument("--dst", required=True, help="副本 B 上导入出来的服务器名字（必须是没用过的新名字）")
    ap.add_argument("--dst-port", type=int, required=True)
    ap.add_argument("--full", action="store_true", help="完整包（带上服务端核心）；不写是瘦身包（核心在导入时重新下载）")
    ap.add_argument("--mode", default="handover", choices=["handover", "distributable"], help="handover=服主交接，distributable=发给朋友")
    ap.add_argument("--mod", default="", help="先用模组购物车给源服务器装一个模组（搜索关键词，如 jei）。只对模组服有意义")
    ap.add_argument("--filler", default="", help="往世界目录里放随机内容的填充文件，写法 个数x每个多少MiB，如 5x1024")
    ap.add_argument("--expect-upload-reject", action="store_true", help="这个包预期会在上传时被拒绝（超过 8 GiB 上限）")
    ap.add_argument("--xmx", type=int, default=4096, help="给服务器的内存（MB）")
    ap.add_argument("--boot-limit", type=int, default=900, help="等开服完成最多几秒")
    ap.add_argument("--keep-pack", action="store_true", help="结束后不删导出的交接包")
    ap.add_argument("--chinese-name", action="store_true", help="给导入出来的服务器起一个带中文和空格的名字（在 --dst 后面加上「 中文名测试」）")
    args = ap.parse_args()
    if args.chinese_name:
        args.dst = args.dst + " 中文名测试"  # 写在脚本里，不经过命令行：有的终端传中文参数会变成乱码

    if args.filler:
        # 填充文件要占四份地方（源服务器、交接包、上传到 B 的那份、导入出来的服务器），上传时系统临时目录还要再放一份
        count, each = args.filler.lower().split("x")
        need = int(count) * int(each) * 1048576
        free_here = shutil.disk_usage(WORK).free
        free_temp = shutil.disk_usage(os.environ.get("TEMP") or WORK).free
        if free_here < need * 4 + (10 << 30) or free_temp < need * 1.2 + (5 << 30):
            note(False, "磁盘空间不够跑这一项：测试目录所在的盘剩 %.0f GB（要 %.0f GB），系统临时目录所在的盘剩 %.0f GB（要 %.0f GB）" % (
                free_here / 1e9, (need * 4 + (10 << 30)) / 1e9, free_temp / 1e9, (need * 1.2 + (5 << 30)) / 1e9))
            finish(args, "磁盘空间不够")

    if B.inst(args.dst):
        note(False, "副本 B 上已经有叫「%s」的服务器，换一个没用过的名字" % args.dst)
        finish(args, "目标名字已被占用")
    save(args.id, "参数.json", vars(args))

    # ---------- 1. 源服务器 ----------
    if not A.inst(args.src):
        build = args.build
        if not build:
            builds = A.data("GET", "/api/builds?core=%s&mc=%s" % (q(args.core), q(args.mc))) or []
            if builds:
                build = next((b["id"] for b in builds if b.get("recommended")), builds[0]["id"])
        code, js = A.call("POST", "/api/instances", {
            "name": args.src, "core": args.core, "mc": args.mc, "build": build,
            "xmxMb": args.xmx, "port": args.src_port, "eula": True, "onlineMode": False})
        if code != 200 or not isinstance(js, dict) or not js.get("ok"):
            save(args.id, "建服接口.json", {"http": code, "body": js})
            note(False, "建源服务器的接口返回 HTTP %s" % code)
            finish(args, "建不了源服务器")
        task = A.wait_task(js["data"]["taskId"], limit=2400)
        save(args.id, "建服任务.json", task)
        note(task_ok(task), "建源服务器 %s %s %s：%s（%.0f 秒）" % (
            args.core, args.mc, build, "完成" if task_ok(task) else "失败 " + str(task.get("error") or "超时"), task.get("seconds", 0)))
        if not task_ok(task):
            finish(args, "源服务器没建成")
    src = A.inst(args.src)
    save(args.id, "源服务器记录.json", src)
    src_dir = A.inst_dir(args.src)
    if str(src.get("core")) != args.core or str(src.get("mc")) != args.mc:
        note(False, "副本 A 上的「%s」是 %s %s，和这次要测的 %s %s 不是一回事" % (args.src, src.get("core"), src.get("mc"), args.core, args.mc))
        finish(args, "源服务器不对")
    if str(src.get("status")) in ("running", "starting"):
        A.stop(args.src)
        A.wait_stopped(args.src)

    level = "world"
    for ln in read_text(os.path.join(src_dir, "server.properties")).splitlines():
        if ln.startswith("level-name="):
            level = ln.split("=", 1)[1].strip() or "world"
    world = os.path.join(src_dir, level)
    if not os.path.exists(os.path.join(world, "level.dat")):
        ok, _ = boot(A, args.src, args.id, "源服务器第一次开服", args.boot_limit)
        if not ok:
            finish(args, "源服务器开不起来，没有世界可交接")

    # ---------- 2. 标记文件、模组、填充文件 ----------
    marker_cfg = os.path.join(src_dir, "config", "handover-marker.toml")
    if not os.path.exists(marker_cfg):
        os.makedirs(os.path.dirname(marker_cfg), exist_ok=True)
        with open(marker_cfg, "w", encoding="utf-8") as f:
            f.write('marker = "%d"\n' % int(time.time()))
    marker_bin = os.path.join(world, "_marker", "marker.bin")
    if not os.path.exists(marker_bin):
        make_filler(marker_bin, 1 << 20)

    mods_dir = os.path.join(src_dir, "mods")
    if args.mod:
        have = [f for f in (os.listdir(mods_dir) if os.path.isdir(mods_dir) else []) if f.lower().endswith(".jar")]
        if have:
            note(None, "源服务器已经有模组 %d 个，不重复装" % len(have))
        else:
            code, js = A.call("GET", "/api/modcart/search?inst=%s&q=%s" % (q(args.src), q(args.mod)))
            save(args.id, "购物车-搜索.json", {"http": code, "body": js})
            data = js.get("data") if isinstance(js, dict) else None
            hits = data.get("hits") if isinstance(data, dict) else data
            pid = ""
            if isinstance(hits, list) and hits:
                pid = hits[0].get("project_id") or hits[0].get("projectId") or ""
            if not pid:
                note(False, "模组购物车搜「%s」没有结果（HTTP %s）" % (args.mod, code))
            else:
                r1 = A.call("POST", "/api/modcart", {"inst": args.src, "projectIds": [pid]})
                code, js = A.call("POST", "/api/instances/%s/modcart/install" % q(args.src),
                                  {"withDeps": True, "includeDraft": False, "expect": [pid]})
                save(args.id, "购物车-加入和安装.json", {"add": r1, "installHttp": code, "install": js})
                if code == 200 and isinstance(js, dict) and (js.get("data") or {}).get("taskId"):
                    task = A.wait_task(js["data"]["taskId"], limit=900)
                    save(args.id, "购物车-安装任务.json", task)
                    jars = [f for f in (os.listdir(mods_dir) if os.path.isdir(mods_dir) else []) if f.lower().endswith(".jar")]
                    note(task_ok(task) and bool(jars), "模组购物车装「%s」：%s，mods 里现在有 %s" % (
                        args.mod, "完成" if task_ok(task) else "失败 " + str(task.get("error")), jars))
                else:
                    note(False, "模组购物车的安装接口返回 HTTP %s" % code)

    big = os.path.join(world, "_bigtest")
    if os.path.isdir(big):
        shutil.rmtree(big)  # 上一轮留下的填充文件：只删这一个自己造的文件夹
    if args.filler:
        count, each = args.filler.lower().split("x")
        count, each = int(count), int(each)
        with Timer() as t:
            for n in range(count):
                make_filler(os.path.join(big, "filler-%02d.bin" % n), each << 20)
        note(None, "造了 %d 个填充文件，每个 %d MiB，合计 %.2f GiB（%.0f 秒）" % (count, each, count * each / 1024.0, t.seconds))

    # ---------- 3. 导出前记下每个文件的指纹 ----------
    compare = [d for d in ("mods", "plugins", "config", "defaultconfigs", "kubejs") if os.path.isdir(os.path.join(src_dir, d))]
    for e in sorted(os.listdir(src_dir)):
        if os.path.isfile(os.path.join(src_dir, e, "level.dat")):
            compare.append(e)
    with Timer() as t:
        before = {d: tree(os.path.join(src_dir, d), ("minedeck/",) if d == "config" else ()) for d in compare}
    total_files = sum(len(v) for v in before.values())
    note(None, "导出前记下指纹：%s，共 %d 个文件（%.0f 秒）" % ("、".join(compare), total_files, t.seconds))

    # ---------- 4. 导出 ----------
    dest = os.path.join(WORK, "packs", args.id)
    os.makedirs(dest, exist_ok=True)
    for old in glob.glob(os.path.join(dest, "*.mdpack")):
        os.remove(old)
    with MemWatch(A) as mem, Timer() as t:
        code, js = A.call("POST", "/api/instances/%s/transfer/export" % q(args.src), {"full": args.full, "mode": args.mode, "dest": dest})
        task = A.wait_task(js["data"]["taskId"], limit=7200) if code == 200 and isinstance(js, dict) and js.get("ok") else {"error": "HTTP %s %s" % (code, js)}
    save(args.id, "导出任务.json", task)
    pack = str(task.get("result") or "")  # 导出任务的 result 就是交接包的位置
    if not task_ok(task) or not pack.lower().endswith(".mdpack") or not os.path.exists(pack):
        note(False, "导出失败：%s" % (task.get("error") or "任务结束了但没有生成 .mdpack"))
        finish(args, "没有交接包")
    left_note = [ln for ln in (task.get("log") or []) if "没有进包" in ln]
    if left_note:
        note(None, "导出时提醒：%s" % left_note[0][:300])
    size = os.path.getsize(pack)
    note(True, "导出%s包（%s）完成：%.2f GiB，%.0f 秒，副本 A 内存 %.0f → 最高 %.0f MB" % (
        "完整" if args.full else "瘦身", args.mode, size / 1073741824.0, t.seconds, mem.start_mb, mem.peak_mb))
    mem_check("导出期间副本 A", mem)

    # ---------- 5. 上传给 B ----------
    cache_dir = os.path.join(B.dir, "cache", "transfer", "import")
    temp_before = temp_multipart()
    with MemWatch(B) as mem, Timer() as t:
        code, js = B.upload_pack(pack)
    save(args.id, "上传响应.json", {"http": code, "body": js, "seconds": t.seconds, "memSamplesMb": mem.samples})
    time.sleep(3)
    left_temp = {p: s for p, s in temp_multipart().items() if p not in temp_before}
    uploaded_ok = code == 200 and isinstance(js, dict) and js.get("ok") and isinstance(js.get("data"), dict) and js["data"].get("file")

    if args.expect_upload_reject:
        note(not uploaded_ok, "超过上限的包上传：%s" % ("被拒绝了（符合预期）" if not uploaded_ok else "居然传上去了"))
        # 这句话说得清不清楚由测试员判断：一个不懂技术的人能不能看出是包超过了 8 GiB 的上限
        note(None, "被拒绝时拿到的回应：HTTP %s，%s（%.0f 秒）" % (code, str(js)[:300], t.seconds))
        left_cache = os.listdir(cache_dir) if os.path.isdir(cache_dir) else []
        note(not left_cache, "被拒绝以后副本 B 的上传目录里%s" % ("是空的" if not left_cache else "还留着 " + str(left_cache)))
        note(not left_temp, "被拒绝以后系统临时目录里%s" % ("没有留下上传的临时文件" if not left_temp else "留下了 " + str({os.path.basename(p): mb(s) for p, s in left_temp.items()})))
        mem_check("上传期间副本 B", mem)
        cleanup(args, big, pack)
        finish(args)

    if not uploaded_ok:
        note(False, "上传失败：HTTP %s，%s" % (code, str(js)[:300]))
        cleanup(args, big, pack)
        finish(args, "交接包传不上去")
    note(True, "上传完成：%.0f 秒（约 %.0f MB/秒），副本 B 内存 %.0f → 最高 %.0f MB" % (
        t.seconds, size / 1048576.0 / max(t.seconds, 0.1), mem.start_mb, mem.peak_mb))
    mem_check("上传期间副本 B", mem)
    note(not left_temp, "上传结束后系统临时目录里%s" % ("没有留下临时文件" if not left_temp else "留下了 " + str({os.path.basename(p): mb(s) for p, s in left_temp.items()})))
    summary = {k: v for k, v in js["data"].items() if k != "file"}
    note(None, "导入向导会看到的摘要：%s" % str(summary)[:600])
    for key in ("core", "mc", "build"):
        note(str(summary.get(key)) == str(src.get(key)), "摘要里的 %s 是 %s（源服务器是 %s）" % (key, summary.get(key), src.get(key)))
    note(summary.get("mode") == args.mode and bool(summary.get("full")) == bool(args.full),
         "摘要里的用途是 %s、%s包（导出时选的是 %s、%s包）" % (summary.get("mode"), "完整" if summary.get("full") else "瘦身", args.mode, "完整" if args.full else "瘦身"))

    # ---------- 6. 导入 ----------
    file_id = js["data"]["file"]
    with MemWatch(B) as mem, Timer() as t:
        code, js = B.call("POST", "/api/transfer/import", {"file": file_id, "name": args.dst, "xmxMb": args.xmx, "port": args.dst_port, "eula": True})
        task = B.wait_task(js["data"]["taskId"], limit=7200) if code == 200 and isinstance(js, dict) and js.get("ok") else {"error": "HTTP %s %s" % (code, js)}
    save(args.id, "导入任务.json", task)
    note(task_ok(task), "导入：%s（%.0f 秒），副本 B 内存最高 %.0f MB" % (
        "完成" if task_ok(task) else "失败 " + str(task.get("error") or "超时"), t.seconds, mem.peak_mb))
    if task.get("warnings"):
        note(None, "导入任务带了提醒：%s" % str(task["warnings"])[:400])
    if not task_ok(task):
        cleanup(args, big, pack)
        finish(args, "导入没成功")
    mem_check("导入期间副本 B", mem)
    left_cache = os.listdir(cache_dir) if os.path.isdir(cache_dir) else []
    note(not left_cache, "导入完成后上传目录里%s" % ("是空的（用完的包已删）" if not left_cache else "还留着 " + str(left_cache)))

    # ---------- 7. 比对 ----------
    dst = B.inst(args.dst)
    save(args.id, "导入后的服务器记录.json", dst)
    dst_dir = B.inst_dir(args.dst)
    for key in ("core", "mc", "build"):
        note(str(src.get(key)) == str(dst.get(key)), "%s：原来 %s，导入后 %s" % (key, src.get(key), dst.get(key)))
    diff = {}
    with Timer() as t:
        for d in compare:
            after = tree(os.path.join(dst_dir, d), ("minedeck/",) if d == "config" else ()) if os.path.isdir(os.path.join(dst_dir, d)) else {}
            missing = sorted(k for k in before[d] if k not in after)
            changed = sorted(k for k in before[d] if k in after and after[k] != before[d][k])
            extra = sorted(k for k in after if k not in before[d])
            diff[d] = {"原来的文件数": len(before[d]), "导入后缺的": missing, "内容变了的": changed, "多出来的": extra}
            note(not missing and not changed, "%s：%d 个文件，缺 %d 个，内容变了 %d 个，多出来 %d 个" % (d, len(before[d]), len(missing), len(changed), len(extra)))
    save(args.id, "文件比对.json", diff)
    props = {}
    for ln in read_text(os.path.join(dst_dir, "server.properties")).splitlines():
        if "=" in ln and not ln.startswith("#"):
            k, v = ln.split("=", 1)
            props[k.strip()] = v.strip()
    note(props.get("server-port") == str(args.dst_port), "server.properties 的端口是 %s（导入时填的是 %d）" % (props.get("server-port"), args.dst_port))
    note(props.get("enable-rcon", "false") == "false", "enable-rcon 是 %s（导入后应该是关着的）" % props.get("enable-rcon", "（没写）"))
    for ctl in ("instance.json",):
        note(os.path.exists(os.path.join(dst_dir, ctl)), "导入后的服务器目录里有 MineDeck 自己重新生成的 %s" % ctl)

    # ---------- 8. 导入出来的服务器开一次 ----------
    ok, _ = boot(B, args.dst, args.id, "导入出来的服务器", args.boot_limit)
    if ok:
        log = read_text(os.path.join(dst_dir, "logs", "latest.log"))
        want = str(src.get("build") or "")
        if want and args.core in ("forge", "neoforge"):
            note(want in log, "开服日志里%s加载器版本 %s" % ("有" if want in log else "没有找到", want))
        if args.mod:
            hit = [ln for ln in log.splitlines() if args.mod.lower() in ln.lower()]
            note(bool(hit), "开服日志里%s「%s」（模组被加载了）" % ("提到了" if hit else "没有提到", args.mod))

    cleanup(args, big, pack)
    finish(args)


def cleanup(args, big, pack):
    """只删这次自己造的东西：源服务器世界里的填充文件夹、导出的交接包。"""
    if args.filler and os.path.isdir(big):
        shutil.rmtree(big)
        note(None, "已删掉源服务器里的填充文件")
    if not args.keep_pack and pack and os.path.exists(pack):
        os.remove(pack)
        note(None, "已删掉导出的交接包（要留着看就加 --keep-pack）")


if __name__ == "__main__":
    try:
        main()
    except SystemExit:
        raise
    except Exception as e:  # 意料之外的错误：记下来，不硬撑
        import traceback
        save("_意外错误", "%d.txt" % int(time.time()), traceback.format_exc())
        print("[不通过] 脚本自己出错了：%s（详情在 results\\_意外错误\\）" % e, flush=True)
        sys.exit(2)
