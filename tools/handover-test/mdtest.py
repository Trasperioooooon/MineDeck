# -*- coding: utf-8 -*-
"""交接测试的辅助模块：只跟测试目录里的两份 MineDeck 副本（A 和 B）说话。

这个文件和 copies.json 放在同一个文件夹（测试目录）里。地址和令牌都从 copies.json 读，
所以它到不了别的端口，也到不了你平时用的那份 MineDeck。只用 Python 自带的库，上传大文件借 Windows 自带的 curl.exe。

在脚本里用：

    from mdtest import A, B, save, sha256_file, make_filler, MemWatch, Timer

    code, body = A.call("GET", "/api/app")              # 返回 (HTTP 状态码, 解出来的 JSON)
    data = A.data("GET", "/api/instances")               # 只要 data，失败就抛异常
    task = A.wait_task(task_id)                          # 等任务结束，返回任务的完整记录
    code, body = B.upload_pack(r"D:\\...\\x.mdpack")     # 上传交接包，body["data"]["file"] 交给导入接口

在命令行里随手查一下：

    python mdtest.py A GET /api/app
    python mdtest.py B GET /api/instances
"""
import hashlib
import io
import json
import os
import re
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

# 这台电脑的控制台不一定是中文代码页：统一按 UTF-8 输出，打不出来的字用 ? 代替，不让脚本因为打印而中断
for _s in (sys.stdout, sys.stderr):
    try:
        _s.reconfigure(encoding="utf-8", errors="replace")
    except Exception:
        pass

# 两份副本都在本机：不管系统里配没配代理，一律直连
_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))

ROOT = os.path.dirname(os.path.abspath(__file__))
RESULTS = os.path.join(ROOT, "results")
WORK = os.path.join(ROOT, "work")
PACKS = os.path.join(ROOT, "packs")
_CURL = os.path.join(os.environ.get("SystemRoot", r"C:\Windows"), "System32", "curl.exe")
if not os.path.exists(_CURL):
    _CURL = "curl.exe"


def q(name):
    """把服务器名字放进接口地址之前先转义（中文、空格都要）。"""
    return urllib.parse.quote(str(name), safe="")


def _copies():
    try:
        with io.open(os.path.join(ROOT, "copies.json"), encoding="utf-8-sig") as f:
            return json.load(f)
    except OSError:
        raise SystemExit("没找到 copies.json：两份测试副本还没有启动。请用户双击「启动测试副本.bat」，不要自己去启动 MineDeck.exe。")


def redact(text):
    """把两份副本的令牌从文字里抹掉。往 results 里写东西之前都过一遍。"""
    text = str(text)
    try:
        for c in _copies().values():
            if c.get("token"):
                text = text.replace(c["token"], "***")
    except Exception:
        pass
    return text


class Copy(object):
    """一份测试副本。key 是 "A" 或 "B"。"""

    def __init__(self, key):
        self.key = key
        self._loaded = False

    def _load(self):
        c = _copies()[self.key]
        host = urllib.parse.urlsplit(c["url"])
        if host.hostname != "127.0.0.1" or host.scheme != "http":
            raise SystemExit("copies.json 里副本 %s 的地址不是本机，不继续" % self.key)
        self.url, self.token, self.dir, self.pid = c["url"], c["token"], c["dir"], int(c.get("pid") or 0)
        if not os.path.abspath(self.dir).lower().startswith(ROOT.lower()):
            raise SystemExit("copies.json 里副本 %s 的目录不在测试目录里，不继续" % self.key)
        self._loaded = True

    def __getattr__(self, name):
        # url / token / dir / pid 第一次用到时才读 copies.json（副本重启以后令牌会变，重新 import 即可）
        if name in ("url", "token", "dir", "pid") and not self.__dict__.get("_loaded"):
            self._load()
            return self.__dict__[name]
        raise AttributeError(name)

    # ---------- 调接口 ----------
    def call(self, method, path, body=None, timeout=180):
        """调一次接口，返回 (HTTP 状态码, 响应)。响应是解出来的 JSON；解不出来就是原文。不抛异常。
        连不上时状态码是 0，响应是出错的说明。"""
        data = None
        headers = {"X-Token": self.token}
        if body is not None:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            headers["Content-Type"] = "application/json; charset=utf-8"
        # 只读的 GET 偶尔连不上时再试两次；会改东西的请求不重试（重试可能把同一件事做两遍）
        tries = 3 if method.upper() == "GET" else 1
        for n in range(tries):
            req = urllib.request.Request(self.url + path, data=data, method=method, headers=headers)
            try:
                with _OPENER.open(req, timeout=timeout) as r:
                    code, raw = r.status, r.read()
                break
            except urllib.error.HTTPError as e:
                code, raw = e.code, e.read()
                break
            except Exception as e:  # 连接被拒、被重置、超时
                if n + 1 >= tries:
                    return 0, "连不上副本 %s：%s" % (self.key, e)
                time.sleep(1.5)
        text = raw.decode("utf-8", "replace")
        try:
            return code, json.loads(text)
        except ValueError:
            return code, text

    def data(self, method, path, body=None, timeout=180):
        """调接口并只取 data。接口没成功就抛 RuntimeError（带状态码和返回的说明）。"""
        code, js = self.call(method, path, body, timeout)
        if code != 200 or not isinstance(js, dict) or not js.get("ok"):
            raise RuntimeError("副本 %s %s %s 没成功：HTTP %s %s" % (self.key, method, path, code, redact(js)[:600]))
        return js.get("data")

    def wait_task(self, task_id, limit=1800, every=3):
        """等一个后台任务结束，返回任务的完整记录（steps、log、error、pending……）。
        超过 limit 秒还没结束，返回的记录里 "timeout" 为 True。"""
        t0 = time.time()
        snap = {}
        while True:
            code, js = self.call("GET", "/api/tasks/" + q(task_id))
            if code == 200 and isinstance(js, dict) and isinstance(js.get("data"), dict):
                snap = js["data"]
                if snap.get("ended"):
                    snap["seconds"] = round(time.time() - t0, 1)
                    return snap
            if time.time() - t0 > limit:
                snap = dict(snap)
                snap["timeout"] = True
                snap["seconds"] = round(time.time() - t0, 1)
                return snap
            time.sleep(every)

    # ---------- 服务器 ----------
    def instances(self):
        return self.data("GET", "/api/instances") or []

    def inst(self, name):
        """按名字找一台服务器的记录，没有就返回 None。"""
        for i in self.instances():
            if i.get("name") == name:
                return i
        return None

    def inst_dir(self, name):
        i = self.inst(name)
        if not i:
            raise RuntimeError("副本 %s 里没有叫「%s」的服务器" % (self.key, name))
        return i.get("dir") or os.path.join(self.dir, "servers", name)

    # 开服和停服的接口不读请求体。带上请求体的话，服务端回完就关连接，Windows 偶尔会把还没读走的响应一起丢掉
    # （客户端看到「连接被重置」，其实服务器已经照做了）。所以这两个不带请求体。
    def start(self, name):
        return self.call("POST", "/api/instances/%s/start" % q(name))

    def stop(self, name):
        return self.call("POST", "/api/instances/%s/stop" % q(name))

    def start_checked(self, name):
        """开服。接口的响应没收到时，再看一眼服务器是不是其实已经在启动了。
        返回 (算不算开始启动了, 说明)。"""
        code, js = self.start(name)
        if code == 200:
            return True, ""
        if code == 0:
            time.sleep(3)
            status = str((self.inst(name) or {}).get("status") or "")
            if status in ("running", "starting"):
                return True, "开服接口的响应没收到（%s），但服务器实际已经在启动" % str(js)[:120]
        return False, "开服接口返回 HTTP %s：%s" % (code, str(js)[:200])

    def wait_log(self, name, pattern=r"Done \(", limit=900, every=3):
        """等这台服务器的 logs/latest.log 里出现 pattern（默认是开服完成的那一行 Done (…）。
        返回 (有没有等到, 用了几秒, 匹配到的那一行)。服务器进程中途没了也会提前返回 False。"""
        log = os.path.join(self.inst_dir(name), "logs", "latest.log")
        rx = re.compile(pattern)
        t0 = time.time()
        seen_running = False
        while time.time() - t0 < limit:
            for line in read_text(log).splitlines():
                if rx.search(line):
                    return True, round(time.time() - t0, 1), line.strip()
            i = self.inst(name) or {}
            status = str(i.get("status") or "")
            if status in ("running", "starting"):
                seen_running = True
            elif seen_running and status in ("stopped", "crashed", "error"):
                return False, round(time.time() - t0, 1), "服务器已经不在运行了（状态 %s）" % status
            time.sleep(every)
        return False, round(time.time() - t0, 1), "等了 %d 秒没等到" % limit

    def wait_stopped(self, name, limit=180, every=2):
        t0 = time.time()
        while time.time() - t0 < limit:
            i = self.inst(name) or {}
            if str(i.get("status") or "") not in ("running", "starting", "stopping"):
                return True
            time.sleep(every)
        return False

    # ---------- 上传（走 curl：几个 GB 的文件不能读进内存） ----------
    def _curl(self, path, forms, timeout):
        out = os.path.join(WORK, "curl-%s-%d.json" % (self.key, int(time.time() * 1000)))
        os.makedirs(WORK, exist_ok=True)
        cmd = [_CURL, "-sS", "--noproxy", "*", "-X", "POST", "-H", "X-Token: " + self.token, "-o", out, "-w", "%{http_code}"]
        for f in forms:
            cmd += ["-F", f]
        cmd.append(self.url + path)
        try:
            r = subprocess.run(cmd, capture_output=True, timeout=timeout)
        except subprocess.TimeoutExpired:
            return 0, "curl 超过 %d 秒没传完" % timeout
        text = read_text(out)
        try:
            os.remove(out)
        except OSError:
            pass
        try:
            code = int((r.stdout or b"0").decode("ascii", "replace").strip()[-3:])
        except ValueError:
            code = 0
        if code == 0:
            return 0, "curl 没连上或中途断了：" + (r.stderr or b"").decode("utf-8", "replace").strip()[:400]
        try:
            return code, json.loads(text)
        except ValueError:
            return code, text

    def upload_pack(self, file, timeout=7200):
        """把一个 .mdpack 交接包传给这份副本，返回 (状态码, 响应)。
        成功时 响应["data"]["file"] 是它存下来的位置，原样交给 POST /api/transfer/import 的 file。"""
        return self._curl("/api/transfer/upload", ['file=@"%s";filename=pack.mdpack' % file], timeout)

    def import_modpack(self, zip_path, name, port, xmx_mb=4096, eula=True, online_mode=False, timeout=7200):
        """导入一个整合包（CurseForge 的 zip 或 Modrinth 的 .mrpack），返回 (状态码, 响应)。
        成功时 响应["data"]["taskId"] 是后台任务的编号，交给 wait_task。
        name 用英文字母和数字（中文名字用界面测）。"""
        ext = os.path.splitext(zip_path)[1].lower() or ".zip"
        forms = [
            'file=@"%s";filename=pack%s' % (zip_path, ext),
            "name=%s" % name,
            "port=%d" % port,
            "xmxMb=%d" % xmx_mb,
            "eula=%s" % ("true" if eula else "false"),
            "onlineMode=%s" % ("true" if online_mode else "false"),
        ]
        return self._curl("/api/import/modpack", forms, timeout)

    # ---------- 进程 ----------
    def mem_mb(self):
        """这份副本的 MineDeck 进程现在占多少内存（MB）。查不到返回 -1。"""
        try:
            out = subprocess.run(["tasklist", "/FI", "PID eq %d" % self.pid, "/FO", "CSV", "/NH"],
                                 capture_output=True, timeout=20).stdout.decode("mbcs", "replace")
        except Exception:
            return -1
        for line in out.splitlines():
            cells = [c.strip('"') for c in line.split('","')]
            if len(cells) >= 5 and cells[1].strip('"') == str(self.pid):
                digits = re.sub(r"[^0-9]", "", cells[4])
                if digits:
                    return round(int(digits) / 1024.0, 1)
        return -1


A = Copy("A")
B = Copy("B")


# ---------- 小工具 ----------
def read_text(path, tail=0):
    """读一个文本文件（日志）。读不了返回空字符串。tail>0 时只要最后 tail 行。"""
    try:
        with open(path, "rb") as f:
            text = f.read().decode("utf-8", "replace")
    except OSError:
        return ""
    if tail > 0:
        return "\n".join(text.splitlines()[-tail:])
    return text


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(8 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def make_filler(path, size_bytes):
    """生成一个装满随机内容的文件（压不小，所以包有多大就是多大），返回它的 SHA-256。
    不要用 fsutil 造全零的文件：全零的几个 GB 压完只剩几 MB，测不到大包。"""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    h = hashlib.sha256()
    left = int(size_bytes)
    with open(path, "wb") as f:
        while left > 0:
            chunk = os.urandom(min(left, 16 << 20))
            f.write(chunk)
            h.update(chunk)
            left -= len(chunk)
    return h.hexdigest()


def tree_hashes(folder):
    """一个文件夹里每个文件的 SHA-256：{相对路径（用 / 分隔）: 哈希}。拿来比对导入前后是不是一模一样。"""
    out = {}
    for base, _dirs, files in os.walk(folder):
        for name in files:
            p = os.path.join(base, name)
            out[os.path.relpath(p, folder).replace("\\", "/")] = sha256_file(p)
    return out


def save(test_id, name, content):
    """把一份证据存到 results/<编号>/<名字>。dict、list 存成 JSON，别的按文字存。令牌会自动抹掉。返回存下的路径。"""
    folder = os.path.join(RESULTS, str(test_id))
    os.makedirs(folder, exist_ok=True)
    path = os.path.join(folder, name)
    if isinstance(content, (dict, list)):
        content = json.dumps(content, ensure_ascii=False, indent=2)
    with io.open(path, "w", encoding="utf-8", newline="\n") as f:
        f.write(redact(content))
    return path


class Timer(object):
    """with Timer() as t: …  之后 t.seconds 是用了几秒。"""

    def __enter__(self):
        self._t0 = time.time()
        self.seconds = 0.0
        return self

    def __exit__(self, *exc):
        self.seconds = round(time.time() - self._t0, 1)
        return False


class MemWatch(object):
    """with MemWatch(B) as m: …  期间每 2 秒记一次这份副本的内存，之后 m.peak_mb 是最高值，m.samples 是全部读数。"""

    def __init__(self, copy, every=2.0):
        self.copy, self.every = copy, every
        self.samples, self.peak_mb = [], -1
        self._stop = threading.Event()
        self._th = None

    def _run(self):
        while not self._stop.is_set():
            v = self.copy.mem_mb()
            if v >= 0:
                self.samples.append(v)
                self.peak_mb = max(self.peak_mb, v)
            self._stop.wait(self.every)

    def __enter__(self):
        self.start_mb = self.copy.mem_mb()
        self._th = threading.Thread(target=self._run, daemon=True)
        self._th.start()
        return self

    def __exit__(self, *exc):
        self._stop.set()
        self._th.join(timeout=30)
        self.end_mb = self.copy.mem_mb()
        return False


def _main(argv):
    if len(argv) < 4 or argv[1] not in ("A", "B"):
        print("用法：python mdtest.py A|B GET|POST|PUT 接口路径 [JSON 文件]")
        print("例子：python mdtest.py A GET /api/app")
        return 2
    copy = A if argv[1] == "A" else B
    # Git Bash 会把 /api/app 这样的参数改写成 C:/Program Files/Git/api/app：把前面多出来的那截去掉
    cut = argv[3].find("/api/")
    if cut > 0:
        argv[3] = argv[3][cut:]
    body = None
    if len(argv) >= 5:
        with io.open(argv[4], encoding="utf-8-sig") as f:
            body = json.load(f)
    code, js = copy.call(argv[2].upper(), argv[3], body)
    print("HTTP", code)
    print(redact(json.dumps(js, ensure_ascii=False, indent=2) if not isinstance(js, str) else js))
    return 0 if code == 200 else 1


if __name__ == "__main__":
    sys.exit(_main(sys.argv))
