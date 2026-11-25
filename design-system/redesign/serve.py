# 看界面重构草图用的静态服务：以仓库根目录为根，所有文件都不缓存（改了草图刷新就是新的）。
# 用法：python design-system/redesign/serve.py [端口=27395]，然后打开 http://127.0.0.1:27395/design-system/redesign/home.html
import functools
import http.server
import os
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))


class NoCache(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store")
        super().end_headers()


port = int(sys.argv[1]) if len(sys.argv) > 1 else 27395
http.server.ThreadingHTTPServer(("127.0.0.1", port), functools.partial(NoCache, directory=ROOT)).serve_forever()
