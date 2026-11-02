package ops

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Record 操作记录的一行（设计文档 §3.4）。
type Record struct {
	T          string          `json:"t"` // RFC 3339，本地时区
	Op         string          `json:"op"`
	Src        Source          `json:"src"`
	Actor      string          `json:"actor,omitempty"`
	Session    string          `json:"session,omitempty"`
	Event      string          `json:"event,omitempty"` // 提案的节点：propose（AI 提出）、reject（人拒绝）、void（作废）；执行的那条没有
	Inst       string          `json:"inst,omitempty"`  // 参数里的实例名（args 里不再重复）
	Args       json.RawMessage `json:"args,omitempty"`  // 脱敏后的参数：Key、密码、凭据一律写成 ***
	Proposal   string          `json:"proposal,omitempty"`
	ApprovedBy string          `json:"approvedBy,omitempty"` // 确认（或拒绝）提案的人
	Reason     string          `json:"reason,omitempty"`     // 拒绝的理由
	OK         bool            `json:"ok"`
	Err        string          `json:"err,omitempty"`
	MS         int64           `json:"ms"`
}

// Log 操作记录：<目录>/2026-11.jsonl，一行一条，按月一个文件；保留最近 12 个月，总共不超过 20 MB
// （超出时从最早的月份删起，当月的文件不删）。
type Log struct {
	MaxMonths int
	MaxBytes  int64

	dir    string
	mu     sync.Mutex
	last   string // 上次写的文件名：换文件时顺手清理旧文件
	writes int    // 写了多少条：程序长时间开着时，每 100 条也清理一次
}

func NewLog(dir string) *Log {
	return &Log{dir: dir, MaxMonths: 12, MaxBytes: 20 << 20}
}

var monthFile = regexp.MustCompile(`^\d{4}-\d{2}\.jsonl$`)

// Write 追加一条记录。写不进去只记进程日志，不影响操作本身。
func (l *Log) Write(rec Record) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return
	}
	t, err := time.Parse(time.RFC3339, rec.T)
	if err != nil {
		t = time.Now()
	}
	name := t.Format("2006-01") + ".jsonl" // 记录写进它自己那个月的文件
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(l.dir, 0o755); err != nil {
		log.Println("写操作记录失败:", err)
		return
	}
	f, err := os.OpenFile(filepath.Join(l.dir, name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Println("写操作记录失败:", err)
		return
	}
	_, err = f.Write(buf.Bytes())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		log.Println("写操作记录失败:", err)
	}
	l.writes++
	if name != l.last || l.writes%100 == 0 {
		l.last = name
		l.pruneLocked(name)
	}
}

// files 记录文件名，从旧到新。
func (l *Log) files() []string {
	ents, _ := os.ReadDir(l.dir)
	var out []string
	for _, e := range ents {
		if !e.IsDir() && monthFile.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out)
	return out
}

// pruneLocked 超过 12 个月、或者总共超过 20 MB 时，从最早的月份删起；current（当月）不删。
func (l *Log) pruneLocked(current string) {
	names := l.files()
	sizes := map[string]int64{}
	var total int64
	for _, n := range names {
		if st, err := os.Stat(filepath.Join(l.dir, n)); err == nil {
			sizes[n] = st.Size()
			total += st.Size()
		}
	}
	for len(names) > 0 && names[0] != current && (len(names) > l.MaxMonths || total > l.MaxBytes) {
		if err := os.Remove(filepath.Join(l.dir, names[0])); err != nil {
			log.Println("清理旧操作记录失败:", err)
			return
		}
		total -= sizes[names[0]]
		names = names[1:]
	}
}

// Query 查询条件。
type Query struct {
	Inst  string // 只看这个实例
	Src   Source // 只看这个来源
	Op    string // 只看这个操作（前缀匹配，如 "instance.players."）
	Limit int    // 最多几条，默认 100，最多 1000
}

// Query 按条件查记录，新的在前。
func (l *Log) Query(q Query) ([]Record, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	q.Limit = min(q.Limit, 1000)
	l.mu.Lock()
	names := l.files()
	l.mu.Unlock()
	out := []Record{}
	for i := len(names) - 1; i >= 0 && len(out) < q.Limit; i-- {
		f, err := os.Open(filepath.Join(l.dir, names[i]))
		if err != nil {
			continue
		}
		var recs []Record
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for sc.Scan() {
			var r Record
			if json.Unmarshal(sc.Bytes(), &r) != nil {
				continue // 写到一半断电留下的半行
			}
			if (q.Inst == "" || r.Inst == q.Inst) && (q.Src == "" || r.Src == q.Src) && (q.Op == "" || strings.HasPrefix(r.Op, q.Op)) {
				recs = append(recs, r)
			}
		}
		f.Close()
		for j := len(recs) - 1; j >= 0 && len(out) < q.Limit; j-- {
			out = append(out, recs[j])
		}
	}
	return out, nil
}

// splitArgs 从规范化后的参数里取出实例名（inst），其余部分脱敏后作为记录的 args。
func splitArgs(a Args) (inst string, rest json.RawMessage) {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(a))
	dec.UseNumber()
	if len(a) == 0 || dec.Decode(&obj) != nil || obj == nil {
		return "", nil
	}
	if s, ok := obj["inst"].(string); ok {
		inst = s
		delete(obj, "inst")
	}
	if len(obj) == 0 {
		return inst, nil
	}
	return inst, json.RawMessage(encode(redact(obj)))
}

// secretName 键名像不像密钥：Key、密码、令牌、凭据，以及可能带令牌的 Webhook 地址。
func secretName(k string) bool {
	k = strings.ToLower(k)
	for _, w := range []string{"password", "passwd", "secret", "token", "credential", "webhook"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return strings.HasSuffix(k, "key") // key、cfApiKey、aiKey……
}

// Redact 把密钥类的值写成 ***（空值保留，看得出是「清除」）。v 是 JSON 解出来的值（map、切片、字符串……）。
// 操作记录用它；交给 AI 的工具结果也用它（再加上 assistant 包的文本脱敏）。
func Redact(v any) any { return redact(v) }

// SecretName 键名像不像密钥（Key、密码、令牌、凭据、Webhook 地址）。
func SecretName(k string) bool { return secretName(k) }

// redact 把密钥类的值写成 ***（空值保留，看得出是「清除」）。两种情况：键名像密钥的字段；
// 以及 {"key": 名称, "value": 值} 这样的键值对里名称像密钥的，比如 server.properties 的 rcon.password。
func redact(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		name, isPair := x["key"].(string)
		_, hasValue := x["value"]
		isPair = isPair && hasValue
		for k, val := range x {
			switch {
			case isPair && k == "key":
				out[k] = val
			case isPair && k == "value" && secretName(name):
				out[k] = mask(val)
			case !isPair && secretName(k):
				out[k] = mask(val)
			default:
				out[k] = redact(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = redact(it)
		}
		return out
	}
	return v
}

func mask(v any) any {
	if v == nil || v == "" {
		return v
	}
	return "***"
}
