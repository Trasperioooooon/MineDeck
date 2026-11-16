## 2026-11-13 入口挪进了 cmd/minedeck（Claude 写）

作者一开始是拿 GitHub 首页的截图来问仓库是不是太乱。补忽略规则、清分支都做完以后，首页那一长串文件还在，作者又问了一次。那些是入库的源码，`.gitignore` 管不了，只有挪进子文件夹首页才会变短。作者要的是首页清爽；挪到 `cmd/minedeck/` 这个摆法是 Claude 挑的，Go 项目里放程序入口的地方通常就叫这个名字。什么时候做是作者定的：先发 3.1.1，发完再挪。

改动在 PR #17（分支 `move-entry-to-cmd`）里，一共三个提交。

第一个只挪位置。根目录的 20 个 Go 文件和 `rsrc_windows_amd64.syso` 用 `git mv` 原样挪进 `cmd/minedeck/`，包名还是 `main`，代码没改。根目录现在只剩两份 README、`LICENSE`、`go.mod`、`go.sum`、`.gitignore` 和 `.gitattributes`。跟着改了四处。`go:embed` 够不到上级目录，而 `assets/icon.ico` 不能跟着挪（官网的发布流程 `pages.yml` 按这个位置用它），所以新加了 `assets/assets.go`，由 `assets` 包把图标内嵌进来，`icon_windows.go` 改成取 `assets.IconICO`，并加了一条测试守住取到的内容和文件一字节不差。`internal/themes` 里有一条测试按相对路径读 `webviewbg_windows.go`，路径跟着改了。`release.yml` 和两份 README 里的构建命令，末尾的 `.` 换成了 `./cmd/minedeck`。`design-system/MASTER.md` 里重新生成图标资源的命令，输出路径也改了。

以后编译要用新命令：`go build -trimpath -ldflags="-s -w -H windowsgui" -o MineDeck.exe ./cmd/minedeck`。原来的 `go build .` 现在会报 no Go files。`release.yml` 里这条命令只在推标签时运行，这次只在本机照着跑过，下一次发版是它第一次在 GitHub 上跑，发版的人留意一下。

第二个提交改的是上一节说的那两处旧说法：`app.js` 里 `guardDrops` 上面的注释，和 `tools/ui-dev/tests.js` 里拖放那条测试的名字。只动了注释和名字。第三个提交就是这一节。

验证是在 3.1.1 发布以后的 main 上做的。gofmt、`go build ./...`、`go test ./...`（34 个包）都过，`go vet` 还是那两处已知误报，只是路径变成了 `cmd\minedeck\` 下面。用发版那条命令挪前挪后各编一个 exe，大小相同，都是窗口程序，exe 的图标资源逐字节相同。挪后的 exe 在全新的文件夹里开了一次真窗口，界面正常，运行时写出的图标和 `assets/icon.ico` 相同；又用接口跑了一遍端口检查，真起了一次服务器再停掉。用的是上一节那套脚本，在 `D:\MineDeck-v310-test\window-auto-311`。界面交互测试（`tools/ui-dev` 的 131 条）这次没有重跑：页面代码只改了一段注释，改完用 `node --check` 看过能解析。

别的分支以后合 main 的时候要注意：在根目录新加的 Go 文件不会自己跟到新位置去，合完要 `git mv` 进 `cmd/minedeck/`，不然编不过；对已有文件的修改会自己跟过去。现在开着的 PR 里，#10（`website-a`）没碰根目录的 Go 文件，#13 改了 `main.go`，它本来就和 main 有冲突。本机别的分支都没有在根目录新加 Go 文件。

### 合并以后

PR #17 的 Test 过了以后，在本机用合并提交合进了 main（`ac2f560`）并推了上去，main 上的 Test 也过了。GitHub 把 PR 记成已合并，远端的 `move-entry-to-cmd` 分支自动删了。用接口看过仓库首页：现在是 7 个文件、10 个文件夹。GitHub 上只剩 main 和 `website-a` 两条分支。

到这里作者那句「先发布然后做」的两件事都做完了。接手的人要知道的几件事：

`a741f4c`（首次启动协议的措辞）已经在 GitHub 上，也在 v3.1.1 里。做它的那个会话不用再推，也不用重做；主仓库的 main 已经跟着快进到了最新。查过整个仓库，原来的两处说法（「非营利」「由社区独立开发」）除了 `app.js` 没有别的地方写着，界面测试、原型、两份 README、官网和功能清单里都没有，不需要跟着改。

界面交互测试（`tools/ui-dev` 的 131 条）在最后发出去的代码上没有重跑。上一次全量跑是 3.1.1 的修复刚做完的时候，那以后页面代码只动了两处：协议里的两行字，和 `guardDrops` 上面的一段注释。CI 不跑这套测试。下次动界面之前跑一遍，心里有底。

本机两条已经合完的分支和它们的工作树，汇报完以后作者说清掉，就清了：`v311-fixes`（工作树 `.claude/worktrees/v310-fixes`）和 `move-entry-to-cmd`（工作树 `.claude/worktrees/move-entry-to-cmd`）。清之前核对过三件事：两条分支的提交都在 main 里，工作树里没有没提交的东西，没有程序占着这两个文件夹。`v310-fixes` 里那个指向主仓库 `tools\ui-dev\node_modules` 的目录联接，先用 `cmd /c rmdir` 单独删掉，再删的工作树；主仓库那份 `node_modules` 前后都是 183 个文件，没受影响。分支都是用 `git branch -d` 删的，没有强删。`move-entry-to-cmd` 第一次没删掉。合并以后 Claude 把这条本机分支快进到了 main 的最新提交，而它记着的那条远端分支已经被 GitHub 自动删掉，本机留的记录还停在合并前的位置；git 拿这条过期的记录来比，认为本机多出来的提交没有合并。先 `git remote prune origin` 把过期的记录清掉再删就行了。`v311-fixes` 没碰上这个问题，它的本机分支和那条过期记录停在同一个提交上。

现在本机剩 9 条分支、8 个工作树：main；`website-a`（PR #10，工作树 `.claude/worktrees/pr10-website`）；vibe 的五个工作区；还有 `claude/eager-cerf-07d023`（检出在工作树 `compassionate-bell-d63e72`）和 `claude/compassionate-bell-d63e72`。最后这两条只在本机，没有合并，没有备份，这次没有动。

主仓库根目录里放过的待办笔记 `docs/待办-入口挪进cmd.md` 从来没有提交过，事情办完已经删了。

清完以后，Claude 又把「交接说明里这处改动要不要提交」做成了提问。作者回的是：「以后关于要不要提交要不要推送这些问题，你自己来判断，不用总是问我。」所以这处改动 Claude 直接提交并推了，以后做完并验证过的改动、交接说明的更新也照这样办，做了在回复里说一声。这句话说的是提交和推送。合并 PR、打标签发版作者没有提，Claude 的理解是那些仍然等作者明说，除非作者交代的事情本身就包含它。

