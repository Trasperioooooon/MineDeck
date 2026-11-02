// testctl 模拟 MineDeck，连上伙伴模组的本机控制端口发指令（协议见 companion/README.md 的「协议 v1」）。
//
//	testctl -config <实例目录>/config/minedeck/companion.properties welcome
//	testctl -config ... send '{"op":"spawn","name":"小木"}'
//	testctl -config ... send -wait -timeout 5m '{"op":"collect","companion":1,"item":"oak_log","count":3}'
//	testctl -config ... send -expect bad_args '{"op":"collect","item":"oak_log;stop"}'
//	testctl -config ... send -raw 'not json' -expect bad_json
//	testctl -config ... watch -for 30s       # 打印这段时间里的所有事件（加 -v 还打印 STATUS）
//	testctl -config ... repl                 # 标准输入一行一条 JSON 指令
//	testctl -config ... demo [-item oak_log] [-count 3] [-keep]
//
// 退出码：0 正常；1 本地错误；2 模组拒绝连接；3 连接中途断开；4 指令失败（或者 -wait 等到的结果不是 done）；5 等事件超时。
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("testctl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	config := fs.String("config", "", "伙伴模组的配置文件 config/minedeck/companion.properties")
	addr := fs.String("addr", "", "直接指定地址（127.0.0.1:端口），和 -token 一起用")
	token := fs.String("token", "", "令牌（不写就用配置文件里的）")
	name := fs.String("name", "testctl", "HELLO 里的客户端名字")
	verbose := fs.Bool("v", false, "也打印每 2 秒一次的 STATUS")
	patience := fs.Duration("patience", 10*time.Second, "连不上（模组还没读到新配置、上一个连接还没走）时最多重试多久")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "用法：testctl -config <companion.properties> welcome|send|watch|repl|demo [参数]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if (*config == "" && *addr == "") || fs.NArg() == 0 {
		fs.Usage()
		return 1
	}
	cl, err := connect(*config, *addr, *token, *name, stdout, *verbose, *patience)
	if err != nil {
		var re *rejectError
		if errors.As(err, &re) {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintf(stderr, "连接失败：%v\n", err)
		return 1
	}
	rest := fs.Args()[1:]
	switch fs.Arg(0) {
	case "welcome":
		cl.close("testctl 看完了")
		return 0
	case "send":
		return cmdSend(cl, rest, stderr)
	case "watch":
		sub := flag.NewFlagSet("watch", flag.ContinueOnError)
		sub.SetOutput(stderr)
		d := sub.Duration("for", 30*time.Second, "看多久")
		if err := sub.Parse(rest); err != nil {
			return 1
		}
		select {
		case <-cl.done:
			return 3
		case <-time.After(*d):
		}
		cl.close("testctl 看完了")
		return 0
	case "repl":
		go func() {
			sc := bufio.NewScanner(stdin)
			sc.Buffer(make([]byte, 64*1024), maxPayload)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				var req map[string]any
				if err := json.Unmarshal([]byte(line), &req); err != nil {
					fmt.Fprintln(stdout, "不是 JSON：", err)
					continue
				}
				go func() { _, _ = cl.call(req, 30*time.Second) }()
			}
			time.Sleep(time.Second)
			cl.close("标准输入结束")
		}()
		<-cl.done
		return 0
	case "demo":
		sub := flag.NewFlagSet("demo", flag.ContinueOnError)
		sub.SetOutput(stderr)
		item := sub.String("item", "oak_log", "要采集的物品")
		count := sub.Int("count", 3, "数量")
		cname := sub.String("name", "MineDeck伙伴", "伙伴的名字")
		timeout := sub.Duration("timeout", 5*time.Minute, "采集最多等多久")
		keep := sub.Bool("keep", false, "做完不移除伙伴")
		if err := sub.Parse(rest); err != nil {
			return 1
		}
		return demo(cl, stdout, *cname, *item, *count, *timeout, *keep)
	default:
		fmt.Fprintf(stderr, "不认识的命令：%s\n", fs.Arg(0))
		cl.close("testctl 不认识的命令")
		return 1
	}
}

func cmdSend(cl *client, args []string, stderr io.Writer) int {
	sub := flag.NewFlagSet("send", flag.ContinueOnError)
	sub.SetOutput(stderr)
	wait := sub.Bool("wait", false, "任务指令：等它的 task_finished")
	timeout := sub.Duration("timeout", 15*time.Second, "等回复（带 -wait 时是等任务结束）最多多久")
	expect := sub.String("expect", "", "期望失败，错误码是这个（对上了退出码 0）")
	raw := sub.Bool("raw", false, "原样发出去（不补 id，不检查是不是 JSON）")
	if err := sub.Parse(args); err != nil || sub.NArg() != 1 {
		fmt.Fprintln(stderr, "用法：testctl ... send [-wait] [-timeout 5m] [-expect 错误码] [-raw] '<JSON>'")
		return 1
	}
	var res map[string]any
	var err error
	if *raw {
		res, err = cl.sendRaw([]byte(sub.Arg(0)), 15*time.Second)
	} else {
		var req map[string]any
		if err := json.Unmarshal([]byte(sub.Arg(0)), &req); err != nil {
			fmt.Fprintf(stderr, "指令不是 JSON：%v\n", err)
			return 1
		}
		res, err = cl.call(req, 15*time.Second)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 3
	}
	ok, _ := res["ok"].(bool)
	if *expect != "" {
		cl.close("testctl 发完了")
		if !ok && res["code"] == *expect {
			return 0
		}
		fmt.Fprintf(stderr, "期望错误码 %s，实际：ok=%v code=%v\n", *expect, ok, res["code"])
		return 4
	}
	if !ok {
		cl.close("testctl 发完了")
		return 4
	}
	if *wait {
		task, _ := res["task"].(map[string]any)
		if task == nil {
			fmt.Fprintln(stderr, "回复里没有 task，没什么可等的")
			cl.close("testctl 发完了")
			return 1
		}
		id := task["id"]
		ev, err := cl.waitEvent(func(m map[string]any) bool {
			t, _ := m["task"].(map[string]any)
			return m["event"] == "task_finished" && t != nil && t["id"] == id
		}, *timeout)
		if err != nil {
			fmt.Fprintln(stderr, err)
			cl.close("testctl 等完了")
			return 5
		}
		cl.close("testctl 等完了")
		if ev["result"] != "done" {
			return 4
		}
		return 0
	}
	cl.close("testctl 发完了")
	return 0
}

func demo(cl *client, out io.Writer, name, item string, count int, timeout time.Duration, keep bool) int {
	res, err := cl.call(map[string]any{"op": "spawn", "name": name}, 15*time.Second)
	if err != nil {
		fmt.Fprintln(out, "spawn 出错：", err)
		return 3
	}
	var id any
	if ok, _ := res["ok"].(bool); ok {
		id = res["companion"]
	} else if res["code"] == "name_taken" {
		st, err := cl.call(map[string]any{"op": "status"}, 15*time.Second)
		if err != nil {
			return 3
		}
		list, _ := st["companions"].([]any)
		for _, c := range list {
			if m, ok := c.(map[string]any); ok && m["name"] == name {
				id = m["id"]
			}
		}
	}
	if id == nil {
		return 4
	}
	// 控制器一般在刷出时就建好了；没好的话等一会
	ready := false
	for i := 0; i < 20 && !ready; i++ {
		st, err := cl.call(map[string]any{"op": "status", "companion": id}, 15*time.Second)
		if err != nil {
			return 3
		}
		if list, ok := st["companions"].([]any); ok && len(list) == 1 {
			c, _ := list[0].(map[string]any)
			ready = c["state"] != "starting"
		}
		if !ready {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if !ready {
		fmt.Fprintln(out, "伙伴的 PlayerEngine 控制器一直没准备好")
		return 4
	}
	res, err = cl.call(map[string]any{"op": "collect", "companion": id, "item": item, "count": count}, 15*time.Second)
	if err != nil {
		return 3
	}
	if ok, _ := res["ok"].(bool); !ok {
		return 4
	}
	taskID := res["task"].(map[string]any)["id"]
	start := time.Now()
	ev, err := cl.waitEvent(func(m map[string]any) bool {
		t, _ := m["task"].(map[string]any)
		return m["event"] == "task_finished" && t != nil && t["id"] == taskID
	}, timeout)
	code := 0
	if err != nil {
		fmt.Fprintln(out, "采集没等到结果：", err)
		code = 5
		_, _ = cl.call(map[string]any{"op": "stop", "companion": id}, 15*time.Second)
	} else {
		fmt.Fprintf(out, "采集结束：result=%v 背包里共 %v 个，这次多了 %v 个（要 %v 个），用时 %.1f 秒\n",
			ev["result"], ev["have"], ev["gained"], ev["want"], time.Since(start).Seconds())
		if ev["result"] != "done" {
			code = 4
		}
	}
	st, err := cl.call(map[string]any{"op": "status", "companion": id, "inventory": true}, 15*time.Second)
	if err == nil {
		if list, ok := st["companions"].([]any); ok && len(list) == 1 {
			c, _ := list[0].(map[string]any)
			b, _ := json.Marshal(c["inventory"])
			fmt.Fprintln(out, "伙伴背包：", string(b))
		}
	}
	if !keep {
		if _, err := cl.call(map[string]any{"op": "despawn", "companion": id}, 15*time.Second); err == nil {
			_, _ = cl.waitEvent(func(m map[string]any) bool { return m["event"] == "removed" }, 15*time.Second)
		}
	}
	cl.close("demo 结束")
	return code
}
