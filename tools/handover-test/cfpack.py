# -*- coding: utf-8 -*-
"""导入一个整合包（CurseForge 的 zip，或 Modrinth 的 .mrpack），把过程和结果记下来。

每一步的检查会打印 [通过] / [不通过] / [信息]，原始证据存进 results/<编号>/。
退出码：0 全部通过，1 有不通过，2 流程走不下去。

例子（在测试目录里运行）：

    python cfpack.py --id C-1 --copy B --zip "packs/某个整合包.zip" --name cf-nokey --port 25761
    python cfpack.py --id C-2 --copy A --zip "packs/某个整合包.zip" --name cf-key --port 25762 --boot
    python cfpack.py --id C-3 --copy A --resolve-task t5 --skip-pending

--copy 选在哪份副本上导入：A 是用户填了 CurseForge Key 的那份，B 没填。
"""
import argparse
import json
import os
import sys
import time
import zipfile

from mdtest import A, B, Timer, q, read_text, save

checks = []


def note(ok, text):
    tag = "[信息]" if ok is None else ("[通过]" if ok else "[不通过]")
    checks.append((tag, text))
    print("%s %s" % (tag, text), flush=True)


def finish(args, aborted=""):
    failed = [c for c in checks if c[0] == "[不通过]"]
    lines = ["%s %s" % c for c in checks]
    verdict = ("中途停了：" + aborted) if aborted else ("有 %d 项不通过" % len(failed) if failed else "全部通过")
    lines.append("总结果：" + verdict)
    save(args.id, "检查结果.txt", "\n".join(lines) + "\n")
    print("总结果：%s（证据在 results\\%s\\）" % (verdict, args.id), flush=True)
    sys.exit(2 if aborted else (1 if failed else 0))


def read_manifest(zip_path):
    """读整合包里的清单，返回 (格式, 概要)。读不出来返回 ("", {})。"""
    try:
        with zipfile.ZipFile(zip_path) as z:
            names = z.namelist()
            if "manifest.json" in names:
                m = json.loads(z.read("manifest.json").decode("utf-8-sig", "replace"))
                mc = m.get("minecraft") or {}
                over = (m.get("overrides") or "overrides").strip("/") + "/"
                return "curseforge", {
                    "名字": m.get("name"), "版本": m.get("version"), "MC": mc.get("version"),
                    "加载器": [x.get("id") for x in mc.get("modLoaders") or []],
                    "清单里的模组数": len(m.get("files") or []),
                    "覆盖目录里的文件数": len([n for n in names if n.startswith(over) and not n.endswith("/")]),
                    "覆盖目录里的 jar 数": len([n for n in names if n.startswith(over + "mods/") and n.lower().endswith(".jar")]),
                }
            if "modrinth.index.json" in names:
                m = json.loads(z.read("modrinth.index.json").decode("utf-8-sig", "replace"))
                return "modrinth", {"名字": m.get("name"), "依赖": m.get("dependencies"), "清单里的文件数": len(m.get("files") or [])}
    except Exception as e:
        return "", {"读不了": str(e)}
    return "", {"读不了": "zip 根目录里没有 manifest.json 或 modrinth.index.json（可能下的是 Server Pack，不是整合包本体）"}


def show_pending(args, task):
    pending = task.get("pending") or []
    save(args.id, "待处理文件.json", pending)
    reasons = {}
    for p in pending:
        reasons[p.get("reason")] = reasons.get(p.get("reason"), 0) + 1
    note(None, "待处理的文件 %d 个，按原因分：%s" % (len(pending), reasons))
    for p in pending[:8]:
        note(None, "  %s｜原因 %s｜%s｜可选处理 %s" % (p.get("id"), p.get("reason"), str(p.get("note") or "")[:90], p.get("options")))
    if len(pending) > 8:
        note(None, "  ……其余的见 results\\%s\\待处理文件.json" % args.id)
    return pending


def main():
    ap = argparse.ArgumentParser(description="导入一个整合包并记录结果")
    ap.add_argument("--id", required=True, help="测试编号，证据存进 results/<编号>/")
    ap.add_argument("--copy", required=True, choices=["A", "B"], help="在哪份副本上导入")
    ap.add_argument("--zip", default="", help="整合包文件的位置")
    ap.add_argument("--name", default="", help="导入出来的服务器名字（英文字母、数字、短横线；必须是没用过的）")
    ap.add_argument("--port", type=int, default=25761)
    ap.add_argument("--xmx", type=int, default=6144, help="给服务器的内存（MB）")
    ap.add_argument("--boot", action="store_true", help="导入完把服务器开一次（等到 Done 或者等到它自己退出）")
    ap.add_argument("--boot-limit", type=int, default=1500)
    ap.add_argument("--resolve-task", default="", help="只处理某个导入任务留下的待处理文件（任务编号，如 t5），不重新导入")
    ap.add_argument("--skip-pending", action="store_true", help="把待处理的文件全部按「跳过」处理")
    args = ap.parse_args()
    copy = A if args.copy == "A" else B

    app = copy.data("GET", "/api/app")
    key_set = bool(app.get("cfKeySet"))
    note(None, "副本 %s：MineDeck %s，CurseForge Key %s%s" % (
        args.copy, app.get("version"), "已填" if key_set else "没填", "（但读不出来，需要重新填）" if app.get("cfKeyBroken") else ""))

    if args.resolve_task:
        task_id = args.resolve_task
        code, js = copy.call("GET", "/api/tasks/" + q(task_id))
        if code != 200 or not isinstance(js, dict) or not isinstance(js.get("data"), dict):
            note(False, "副本 %s 上没有任务 %s（HTTP %s）。副本重启过的话，待处理清单就没有了，这是已知情况" % (args.copy, task_id, code))
            finish(args, "找不到任务")
        task = js["data"]
    else:
        if not args.zip or not args.name:
            note(False, "要导入就得写 --zip 和 --name")
            finish(args, "参数不全")
        args.zip = os.path.abspath(args.zip)
        if not os.path.isfile(args.zip):
            note(False, "找不到整合包文件：%s" % args.zip)
            finish(args, "没有整合包")
        if copy.inst(args.name):
            note(False, "副本 %s 上已经有叫「%s」的服务器，换一个没用过的名字" % (args.copy, args.name))
            finish(args, "名字已被占用")
        fmt, man = read_manifest(args.zip)
        save(args.id, "整合包清单概要.json", {"格式": fmt, "概要": man, "文件大小": os.path.getsize(args.zip)})
        note(bool(fmt), "整合包：%s 格式，%.1f MB，%s" % (fmt or "认不出", os.path.getsize(args.zip) / 1048576.0, man))
        if not fmt:
            finish(args, "这个文件不是整合包本体")

        with Timer() as t:
            code, js = copy.import_modpack(args.zip, args.name, args.port, xmx_mb=args.xmx)
        save(args.id, "导入接口响应.json", {"http": code, "body": js, "seconds": t.seconds})
        if code != 200 or not isinstance(js, dict) or not js.get("ok"):
            note(False, "导入接口返回 HTTP %s：%s" % (code, str(js)[:300]))
            finish(args, "导入没有开始")
        task_id = js["data"]["taskId"]
        note(None, "导入任务编号 %s（处理待处理文件时要用）" % task_id)
        task = copy.wait_task(task_id, limit=7200)
        save(args.id, "导入任务.json", task)
        ok = bool(task.get("ended")) and not task.get("error") and not task.get("timeout")
        note(ok, "导入：%s（%.0f 秒）" % ("完成" if ok else "失败 " + str(task.get("error") or "超时"), task.get("seconds", 0)))
        for s in task.get("steps") or []:
            if s.get("status") != "done":
                note(None, "  步骤「%s」的状态是 %s" % (s.get("name"), s.get("status")))
        for w in task.get("warnings") or []:
            note(None, "提醒：%s（%d 项）%s" % (w.get("title"), len(w.get("items") or []), str(w.get("note") or "")[:120]))
        save(args.id, "导入日志.txt", "\n".join(task.get("log") or []))
        if not ok:
            finish(args, "导入没成功")

        inst = copy.inst(args.name)
        save(args.id, "服务器记录.json", inst)
        inst_dir = copy.inst_dir(args.name)
        mods_dir = os.path.join(inst_dir, "mods")
        jars = sorted(f for f in (os.listdir(mods_dir) if os.path.isdir(mods_dir) else []) if f.lower().endswith(".jar"))
        save(args.id, "mods目录.json", jars)
        note(None, "导入出来的服务器：%s %s %s，mods 里有 %d 个 jar" % (inst.get("core"), inst.get("mc"), inst.get("build"), len(jars)))
        pending = show_pending(args, task)
        if fmt == "curseforge":
            want = man.get("清单里的模组数", 0)
            # 清单里的每一项要么下载到了，要么在待处理里。客户端专用的资源包、光影不进 mods，所以这里只提示，不判不通过
            note(None, "清单里 %d 项；mods 里 %d 个 jar（其中覆盖目录自带 %d 个）；待处理 %d 个" % (
                want, len(jars), man.get("覆盖目录里的 jar 数", 0), len(pending)))
            if not key_set:
                note(len(pending) > 0, "没填 Key：%s" % ("模组进了待处理清单，没有让整个导入失败" if pending else "没有任何待处理文件，和预期不符"))
                said = [ln for ln in (task.get("log") or []) if "API Key" in ln]
                note(bool(said), "没填 Key：任务日志里%s" % ("写明了和 API Key 有关：" + said[0][:120] if said else "没有提到 API Key，用户不知道为什么"))
            else:
                note(len(jars) > 0, "填了 Key：%s" % ("下载到了模组" if jars else "一个模组都没下载到"))
        for report in ("导入待决清单.txt", "未解析模组清单.txt"):
            p = os.path.join(inst_dir, report)
            if os.path.exists(p):
                save(args.id, report, read_text(p))
                note(None, "服务器目录里有《%s》（已存进证据）" % report)

    # ---------- 待处理文件 ----------
    if args.skip_pending:
        pending = task.get("pending") or []
        if not pending:
            note(None, "没有待处理的文件，不用跳过")
        else:
            items = [{"id": p.get("id"), "act": "skip"} for p in pending]
            code, js = copy.call("POST", "/api/import/pending/" + q(task_id), {"items": items})
            save(args.id, "跳过待处理-响应.json", {"http": code, "body": js})
            note(code == 200, "把 %d 个待处理文件按「跳过」处理：HTTP %s" % (len(items), code))
            code, js = copy.call("GET", "/api/tasks/" + q(task_id))
            left = (js.get("data") or {}).get("pending") or [] if isinstance(js, dict) else []
            note(not left, "处理完以后任务里还剩 %d 个待处理文件" % len(left))

    # ---------- 开一次 ----------
    if args.boot and args.name:
        started, why = copy.start_checked(args.name)
        if not started:
            note(False, why)
        else:
            if why:
                note(None, why)
            ok, secs, line = copy.wait_log(args.name, limit=args.boot_limit)
            inst_dir = copy.inst_dir(args.name)
            save(args.id, "latest.log末尾.txt", read_text(os.path.join(inst_dir, "logs", "latest.log"), tail=200))
            crash_dir = os.path.join(inst_dir, "crash-reports")
            crashes = sorted(os.listdir(crash_dir)) if os.path.isdir(crash_dir) else []
            if crashes:
                save(args.id, "崩溃报告-开头.txt", "\n".join(read_text(os.path.join(crash_dir, crashes[-1])).splitlines()[:80]))
            # 整合包里常有只能装在客户端的模组，服务器开不起来不一定是 MineDeck 的问题：这里只记录，由测试员看日志判断
            note(None, "开服：%s（%.0f 秒）%s%s" % ("完成" if ok else "没有开起来", secs, line[:140], "；有崩溃报告 " + crashes[-1] if crashes else ""))
            copy.stop(args.name)
            stopped = copy.wait_stopped(args.name, limit=300)
            note(True if stopped else False, "停服%s" % ("完成" if stopped else "超过 5 分钟还没停下来"))
    finish(args)


if __name__ == "__main__":
    try:
        main()
    except SystemExit:
        raise
    except Exception as e:
        import traceback
        save("_意外错误", "%d.txt" % int(time.time()), traceback.format_exc())
        print("[不通过] 脚本自己出错了：%s（详情在 results\\_意外错误\\）" % e, flush=True)
        sys.exit(2)
