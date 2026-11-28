package inst

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// Schedule 定时任务：每天 At（HH:MM）执行一次，或者每隔 Every 分钟执行一次，两样只填一样
// （老配置只有 at，照旧是每天定时）。Group 是界面上的文件夹名，只管怎么分组显示，不影响执行。
//
// 每隔多久按钟点对齐：从每天 0 点起，0 点、Every 分钟、2×Every 分钟……到点就执行（每隔 60 分钟就是每个整点）。
// 不从开服或保存那一刻起算：那样要记「上次是什么时候」，重开 MineDeck、两个窗口各存一次、崩溃重启都会把节奏打乱；
// 对齐钟点不用记状态，改了间隔下一次就是下一个对得上的钟点，界面也能照着写出几点执行。
type Schedule struct {
	Type  string `json:"type"`            // restart | command | backup
	At    string `json:"at,omitempty"`    // "HH:MM"：每天这个时刻
	Every int    `json:"every,omitempty"` // 分钟：每隔这么久（和 At 二选一）
	Args  string `json:"args,omitempty"`
	Group string `json:"group,omitempty"` // 文件夹名；空的不放文件夹
}

// 每隔多久的下限（分钟）。命令（扫地、提醒）5 分钟就够密了；备份 30 分钟：大世界压一次要几分钟，热备期间还暂停着自动保存，
// 定时备份又按定时备份的保留份数（默认 24 份，所有服务器共用一个设置；手动备份另算，见 pruneBackups）滚动删最旧的，
// 太密了留下的全是最近一小会儿的；
// 重启 60 分钟：光倒计时、停服、再启动就要两三分钟。
var everyMin = map[string]int{"command": 5, "backup": 30, "restart": 60}

// everyMax 每隔多久的上限：12 小时。按钟点对齐，再长就只剩每天 0 点那一次，那用每天定时更清楚。
const everyMax = 720

// maxGroupLen 文件夹名最多几个字。
const maxGroupLen = 32

var typeTitles = map[string]string{"restart": "定时重启", "command": "定时命令", "backup": "定时备份"}

// Policies 实例运维策略。
type Policies struct {
	CrashRestart bool       `json:"crashRestart"`
	Schedules    []Schedule `json:"schedules,omitempty"`
}

// AutoAction 程序自己执行的一次操作（定时任务、崩溃自动重启），交给操作层写一条操作记录（来源 schedule），
// 事后能查到「服务器是谁重启的」。
type AutoAction struct {
	Op    string         // 对应的操作名，如 instance.restart
	Actor string         // "定时任务"、"崩溃自动重启"
	Args  map[string]any // 含实例名 inst
	Err   error
	Dur   time.Duration
}

// SetAutoHook 登记自动操作的回调（操作层在启动时注入）。
func (m *Manager) SetAutoHook(fn func(AutoAction)) {
	m.autoMu.Lock()
	m.autoHook = fn
	m.autoMu.Unlock()
}

func (m *Manager) noteAuto(a AutoAction) {
	m.autoMu.Lock()
	fn := m.autoHook
	m.autoMu.Unlock()
	if fn != nil {
		fn(a)
	}
}

// normHHMM 把「9:05」「09:5」这类时刻规整成补零的 HH:MM；格式不对或越界时 ok 为假。
func normHHMM(s string) (string, bool) {
	var h, mi int
	var rest string
	if n, _ := fmt.Sscanf(strings.TrimSpace(s)+" .", "%d:%d %s", &h, &mi, &rest); n != 3 || rest != "." || h < 0 || h > 23 || mi < 0 || mi > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", h, mi), true
}

// PolicyPatch 改运维策略：只改传了的。
//
// 两个窗口同时开着同一台服务器的「定时任务」时，以前每次保存都把整页的开关和整张任务表发过来、整个换掉，
// 没刷新的那一边一保存，就把另一边刚加的任务删掉、刚删的又加回来（2026-11-17）。现在界面只发改了什么：
// 加一条发 Add，删一条发 Remove，拨开关发 CrashRestart；Schedules 是老写法（AI、MCP 或者还开着旧页面的人），给了就整张替换。
type PolicyPatch struct {
	CrashRestart *bool       `json:"crashRestart"`    // 给了才改
	Schedules    *[]Schedule `json:"schedules"`       // 给了就整张替换
	Remove       []Schedule  `json:"removeSchedules"` // 每一项删掉一条完全相同的，找不到的不算错
	Add          []Schedule  `json:"addSchedules"`    // 按条追加
}

// checkSchedules 检查要存进去的任务（类型、时刻或间隔、命令不能空、文件夹名），返回规整以后的副本
// （时刻补零、文件夹名去掉首尾空白），不改传进来的切片。
func checkSchedules(in []Schedule) ([]Schedule, error) {
	out := slices.Clone(in)
	for k, s := range out {
		if s.Type != "restart" && s.Type != "command" && s.Type != "backup" {
			return nil, fmt.Errorf("未知任务类型: %s", s.Type)
		}
		switch {
		case s.Every != 0 && strings.TrimSpace(s.At) != "":
			return nil, fmt.Errorf("每天几点（at）和每隔多久（every）只能填一样")
		case s.Every == 0 && strings.TrimSpace(s.At) == "": // AI、MCP 两样都漏填时，说清楚要填什么，而不是「时间格式应为 HH:MM: 」
			return nil, fmt.Errorf("每天几点（at）和每隔多久（every）要填一样")
		case s.Every < 0:
			return nil, fmt.Errorf("间隔应为正整数（分钟）: %d", s.Every)
		case s.Every > 0:
			if lo := everyMin[s.Type]; s.Every < lo {
				return nil, fmt.Errorf("%s最短每隔 %d 分钟一次: %d", typeTitles[s.Type], lo, s.Every)
			}
			if s.Every > everyMax {
				return nil, fmt.Errorf("间隔最长 %d 分钟（12 小时），更长的请用每天定时: %d", everyMax, s.Every)
			}
			out[k].At = ""
		default:
			at, ok := normHHMM(s.At)
			if !ok {
				return nil, fmt.Errorf("时间格式应为 HH:MM: %s", s.At)
			}
			out[k].At = at // 存成补零的 HH:MM：调度按 "15:04" 逐字比对，「9:05」原来永远不会触发
		}
		if s.Type == "command" && s.Args == "" {
			return nil, fmt.Errorf("定时命令不能为空")
		}
		g := strings.TrimSpace(s.Group)
		if utf8.RuneCountInString(g) > maxGroupLen {
			return nil, fmt.Errorf("文件夹名最多 %d 个字: %s", maxGroupLen, g)
		}
		if strings.ContainsFunc(g, unicode.IsControl) {
			return nil, fmt.Errorf("文件夹名不能有换行之类的控制字符")
		}
		out[k].Group = g
	}
	return out, nil
}

// sameSchedule 删除时认的「同一条」：类型、规整以后的时刻、间隔、命令、文件夹都一样（文件夹两边都去掉首尾空白再比）。
// 文件夹也算：同一条命令一份在「扫地」里、一份不放文件夹，删哪一份要分得清；挪文件夹也是删掉原处那条、在新文件夹里加一条。
// 两边的时刻都规整一遍：老配置里可能存着没补零的「9:05」，界面上发来的也可能是「9:05」。删除项 r 的时刻不合法就谁也对不上。
func sameSchedule(r, s Schedule) bool {
	if r.Type != s.Type || r.Every != s.Every || r.Args != s.Args || strings.TrimSpace(r.Group) != strings.TrimSpace(s.Group) {
		return false
	}
	if r.Every > 0 && strings.TrimSpace(r.At) == "" { // 每隔多久的：没有时刻
		return strings.TrimSpace(s.At) == ""
	}
	rt, ok := normHHMM(r.At)
	st, _ := normHHMM(s.At)
	return ok && rt == st
}

// Interval 调度实际用的间隔（分钟），0 表示按每天的时刻。存着的任务不再检查（手改过的也原样留着，见 PolicyPatch.Apply），
// 所以这里兜底：时刻和间隔都有的，照老写法按时刻算；间隔越界的，按最短、最长算，不让手改出来的「每 1 分钟备份」把备份全挤掉。
// 调度、操作记录、AI 预览里写的任务都用它，写出来的就是实际怎么跑的。
func (s Schedule) Interval() int {
	if s.Every <= 0 || strings.TrimSpace(s.At) != "" {
		return 0
	}
	return min(max(s.Every, everyMin[s.Type], 1), everyMax)
}

// dueAt 这条任务在 now 这一分钟该不该执行。每天定时的：时刻对得上。每隔多久的：从当天 0 点起过了的分钟数是间隔的整数倍
// （每隔 60 分钟就是每个整点，每隔 90 分钟是 0:00、1:30、3:00……；除不尽一天的，到 0 点重新数，最后一段短一些）。
func dueAt(s Schedule, now time.Time) bool {
	if every := s.Interval(); every > 0 {
		return (now.Hour()*60+now.Minute())%every == 0
	}
	at, _ := normHHMM(s.At) // 老配置里可能存着没补零的「9:05」
	return at != "" && at == now.Format("15:04")
}

// Apply 把改动合到 cur 上，返回改完以后的样子；不改 cur。顺序：整张替换 → 按条删 → 按条加。
// 预览和真正保存用的都是它，预览里看到的就是存进去的。只检查这次传进来的任务，存着的老任务原样保留
// （手改过的、没补零的都不动，免得一条老数据挡住「只拨一下崩溃重启」）。
func (p PolicyPatch) Apply(cur Policies) (Policies, error) {
	out := cur // 整份拷过来再改（以后 Policies 多了字段也带着），任务表另拷一份，不和 cur 共用
	out.Schedules = slices.Clone(cur.Schedules)
	if p.CrashRestart != nil {
		out.CrashRestart = *p.CrashRestart
	}
	if p.Schedules != nil {
		ss, err := checkSchedules(*p.Schedules)
		if err != nil {
			return cur, err
		}
		out.Schedules = ss
	}
	for _, r := range p.Remove {
		// 一项只删一条：两条一模一样的任务，删一次还剩一条
		if k := slices.IndexFunc(out.Schedules, func(s Schedule) bool { return sameSchedule(r, s) }); k >= 0 {
			out.Schedules = slices.Delete(out.Schedules, k, k+1)
		}
	}
	if len(p.Add) > 0 {
		ss, err := checkSchedules(p.Add)
		if err != nil {
			return cur, err
		}
		out.Schedules = append(out.Schedules, ss...)
	}
	return out, nil
}

// PatchPolicies 改实例策略（见 PolicyPatch），返回改完以后的样子。读当前的、合并、存盘在 procMu 里一次做完：
// 两个窗口差不多同时保存，后到的那次合在先到的那次存好的结果上。存盘失败时内存里的也不改。
func (m *Manager) PatchPolicies(name string, p PolicyPatch) (Policies, error) {
	i, err := m.Get(name)
	if err != nil {
		return Policies{}, err
	}
	// 不看现状就能查出来的错（类型、时刻、命令为空）先报，和以前一样不必先去登记「保存实例设置」
	if _, err := p.Apply(Policies{}); err != nil {
		return Policies{}, err
	}
	release, err := m.UseWorldInstance(i, "保存实例设置")
	if err != nil {
		return Policies{}, err
	}
	defer release()
	i.procMu.Lock()
	defer i.procMu.Unlock()
	next, err := p.Apply(i.Policies)
	if err != nil {
		return Policies{}, err
	}
	old := i.Policies
	i.Policies = next
	if err := m.saveInstance(i); err != nil { // 持锁落盘，避免与并发写竞争地读写 i.Settings
		i.Policies = old
		return Policies{}, err
	}
	next.Schedules = slices.Clone(next.Schedules) // 交出去的和存着的不共用一块内存
	return next, nil
}

// UpdatePolicies 整体替换实例策略：崩溃重启和整张任务表都换成 p 的（等于两项都给了的 PatchPolicies）。
func (m *Manager) UpdatePolicies(name string, p Policies) error {
	ss := p.Schedules
	_, err := m.PatchPolicies(name, PolicyPatch{CrashRestart: &p.CrashRestart, Schedules: &ss})
	return err
}

var crashRestartDelays = []time.Duration{10 * time.Second, 30 * time.Second, 60 * time.Second}

// crashRestart 崩溃后按退避策略自动重启（10s/30s/60s，连续 3 次放弃；
// 正常运行超过 10 分钟后计数清零）。gen 为崩溃那次运行的代号：只要期间又发生过任何
// 新的运行（用户手动启动/停止、或上一次退避重启），本次重启即作废，避免把用户主动
// 停掉的服务器又拉起来。端口还被占着时，按同样的退避间隔和次数上限接着排（占连崩次数，不无限重试）。
func (m *Manager) crashRestart(i *Instance, gen int64) {
	maxTries := len(crashRestartDelays)
	handled := func() bool { // 用户已手动处理（重启过或正在运行）
		i.procMu.Lock()
		defer i.procMu.Unlock()
		return i.runGen != gen || (i.state != "" && i.state != "stopped")
	}
	var waitPort int
	for first := true; ; first = false {
		i.procMu.Lock()
		if i.runGen != gen || (i.state != "" && i.state != "stopped") {
			// 期间已有新的运行发生（用户手动启停/上次退避重启），本次作废：
			// 在自增计数与清零之前就返回，避免污染当前运行的连崩计数与清零时基。
			i.procMu.Unlock()
			return
		}
		if first && time.Since(i.startedAt) > 10*time.Minute {
			i.crashCount = 0
		}
		i.crashCount++
		n := i.crashCount
		i.procMu.Unlock()
		if n > maxTries {
			i.Console.Append(fmt.Sprintf("[MineDeck] ⚠ 连续崩溃 %d 次，已停止自动重启。请检查上方日志排查原因。", maxTries))
			return
		}
		delay := crashRestartDelays[n-1]
		if first {
			i.Console.Append(fmt.Sprintf("[MineDeck] 检测到服务器异常退出，%v 后自动重启（第 %d/%d 次）...", delay, n, maxTries))
		} else {
			i.Console.Append(fmt.Sprintf("[MineDeck] 仍在等待端口 %d 放开，%v 后再试自动重启（第 %d/%d 次）...", waitPort, delay, n, maxTries))
		}
		time.Sleep(delay)
		if handled() {
			return
		}
		m.waitPortFree(i) // 端口一般早放开了；还占着就再等几秒，不让一下子的状态把这次重启取消掉
		if handled() {
			return
		}
		start := time.Now()
		err := m.Start(i.Name)
		var busy *portBusyError
		retryPort := err != nil && errors.As(err, &busy) && n < maxTries
		if retryPort {
			waitPort = busy.port
		}
		if err != nil {
			gaveUp := ""
			if !retryPort {
				gaveUp = restartGaveUp(err)
			}
			i.Console.Append("[MineDeck] 自动重启失败: " + err.Error() + gaveUp)
		}
		m.noteAuto(AutoAction{Op: "instance.start", Actor: "崩溃自动重启", Args: map[string]any{"inst": i.Name, "attempt": n}, Err: err, Dur: time.Since(start)})
		if !retryPort {
			return
		}
	}
}

// startScheduler 全局定时任务调度（每 20 秒检查一次到点任务）。
func (m *Manager) startScheduler() {
	st := newSchedState(m.execSchedule)
	go func() {
		tick := time.NewTicker(20 * time.Second)
		defer tick.Stop()
		for range tick.C {
			m.schedTick(st, time.Now())
		}
	}()
}

// schedState 调度循环自己记的东西。minute、fired、seen 只有调度那一个 goroutine 读写；busy 执行完的 goroutine 也要改，单独上锁。
type schedState struct {
	minute string                    // 上一轮看的是哪一分钟（"2006-01-02 15:04"）
	fired  map[string]string         // 交出去过的任务、「服务器 + 类型」→ 在哪一分钟；只留这一分钟和上一分钟的，见 schedTick
	seen   map[*Instance]*backupSeen // 有每隔多久备份的服务器 → 世界自上次定时备份以后变没变过，见 schedTick
	run    func(*Instance, Schedule) // 执行一条任务；测试里换成假的

	busyMu sync.Mutex
	busy   map[string]bool // 还没做完的：备份、重启按「服务器 + 类型」，命令按任务（见 schedTick）
}

// backupSeen 一台服务器上一次交出定时备份时的运行代际，以及那以后看到过它开着没有。
type backupSeen struct {
	gen    int64
	active bool
}

func newSchedState(run func(*Instance, Schedule)) *schedState {
	return &schedState{fired: map[string]string{}, seen: map[*Instance]*backupSeen{}, busy: map[string]bool{}, run: run}
}

// schedID 调度认任务用的内容：实例、类型、时刻、间隔、命令（schedTick 再加上第几条一样的）。不按第几条认：删掉前面一条，
// 后面的序号全变了，原来会把刚执行过的当成没执行过、或者反过来。时刻、间隔都按实际怎么跑的写（补零的时刻、Interval），
// 手改的「9:05」在这一分钟里被界面整表保存规整成「09:05」，也还认得出是同一条。文件夹不算：这一分钟里挪个文件夹，不该再执行一次。
func schedID(name string, s Schedule) string {
	at, ok := normHHMM(s.At)
	if !ok {
		at = strings.TrimSpace(s.At)
	}
	every := s.Interval()
	if every > 0 {
		at = ""
	}
	return fmt.Sprintf("%s|%s|%s|%d|%s", name, s.Type, at, every, s.Args)
}

// claim 登记这条任务开始执行；上一次还没做完就返回 false。
func (st *schedState) claim(key string) bool {
	st.busyMu.Lock()
	defer st.busyMu.Unlock()
	if st.busy[key] {
		return false
	}
	st.busy[key] = true
	return true
}

func (st *schedState) done(key string) {
	st.busyMu.Lock()
	delete(st.busy, key)
	st.busyMu.Unlock()
}

// schedTick 看一遍所有服务器的任务，把 now 这一分钟到点的交出去执行。
//
// 同一台服务器同一时刻只做一个备份、一个重启（按「服务器 + 类型」占位，不看时刻、间隔、命令）：
//   - 这一分钟里已经交出去一个备份（比如每天 04:00 的和每小时的同时到点），别的备份这一分钟不再做，也不说什么——
//     同一份世界接连压两遍没有用，还按保留份数多挤掉一份旧的；重启同理，两个重启叠在一起会停两次、起两次。
//   - 上一次的还没做完就跳过这一次，在控制台说一声：定时备份撞上启动、停止要等服务器稳定下来，
//     最多能等十几分钟、再试两次，每隔 30 分钟的备份就可能叠上下一次。
//
// 每隔多久的备份，服务器停着时不备份：世界没变，备出来的和上一份一样，还按保留份数把更早的定时备份挤掉，
// 停一晚上就只剩一样的那几份了。按服务器记（seen），不按任务记：说的是「世界自上次定时备份以后变没变过」，
// 改间隔、删了再加、两条每隔多久的备份都共用这一份。只在这几种情况备份：到点时开着；上次定时备份以后开过
// （这一段的改动还没备过，停下以后补一份）；上次定时备份以后又启动过（两次查看之间开了又停的，运行代际变了）。
// 真把备份交出去了才算「备过了」：跳过的（上一次还没做完）不算，不然那一份要补的就丢了。每天定时的备份照旧，停着也备，
// 它交出去了也算备过。第一次看到（刚加上每隔多久的备份、程序刚启动）时从那一刻的样子算起：程序启动以后一直停着的服务器不备份。
func (m *Manager) schedTick(st *schedState, now time.Time) {
	stamp := now.Format("2006-01-02 15:04")
	if stamp != st.minute {
		// fired 不一直攒着：只留这一分钟和上一分钟的（时钟往回拨一点，刚执行过的也还认得出来）
		floor := min(stamp, st.minute)
		for k, v := range st.fired {
			if v < floor {
				delete(st.fired, k)
			}
		}
		st.minute = stamp
	}
	keepSeen := map[*Instance]bool{}
	for _, i := range m.List() {
		i.procMu.Lock()
		scheds := slices.Clone(i.Policies.Schedules)
		gen, state := i.runGen, i.state
		i.procMu.Unlock()
		on := state != "" && state != "stopped" // 运行中、启动中、停止中都算开着：世界可能在变
		bs := st.seen[i]
		if slices.ContainsFunc(scheds, func(s Schedule) bool { return s.Type == "backup" && s.Interval() > 0 }) {
			keepSeen[i] = true
			if bs == nil {
				bs = &backupSeen{gen: gen}
				st.seen[i] = bs
			}
			bs.active = bs.active || on
		}
		nth := map[string]int{}
		for _, s := range scheds {
			id := schedID(i.Name, s)
			nth[id]++
			key := fmt.Sprintf("%s#%d", id, nth[id])
			if !dueAt(s, now) || st.fired[key] == stamp {
				continue
			}
			st.fired[key] = stamp
			if s.Type == "backup" && s.Interval() > 0 && !bs.active && bs.gen == gen && !on {
				continue // 停着，上次定时备份以后也没开过
			}
			lock := key
			if s.Type == "backup" || s.Type == "restart" {
				lock = i.Name + "|" + s.Type
				if st.fired[lock] == stamp {
					continue // 这一分钟这台服务器已经有一个了
				}
				st.fired[lock] = stamp
			}
			if !st.claim(lock) {
				i.Console.Append(fmt.Sprintf("[MineDeck] 上一次%s还没做完，这一次跳过", typeTitles[s.Type]))
				continue
			}
			if s.Type == "backup" && bs != nil {
				bs.gen, bs.active = gen, on
			}
			go func() {
				defer st.done(lock)
				st.run(i, s)
			}()
		}
	}
	for i := range st.seen { // 已经没有每隔多久备份的服务器（包括删掉的）不再记
		if !keepSeen[i] {
			delete(st.seen, i)
		}
	}
}

// 定时备份撞上启动、停止时：最多等 backupSettleWait；服务器要连续 backupSettleQuiet 这么久一直是运行中、
// 或者一直是已停止，才算稳定下来；每隔 backupSettlePoll 看一次。测试里改短，settleNow、settleSleep 换成假的时钟。
var (
	backupSettleWait  = 15 * time.Minute
	backupSettleQuiet = 30 * time.Second
	backupSettlePoll  = 2 * time.Second
	settleNow         = time.Now
	settleSleep       = time.Sleep
)

// waitSettled 等服务器稳定下来，等到了返回 true。
//
// 一看到「已停止」就动手是不行的：定时重启在停和启之间、「停止并还原」在停下和还原之间、退出 MineDeck 时
// 关服以后，都会经过一小段「已停止」。这时开始冷备，要么压到一半服务器又启动了（白压一遍还被作废），要么
// 抢在还原、删除前面登记了「正在备份」把它们挡掉，要么程序随即退出、留下半截的临时文件。所以要连续一段时间
// 不变；中途又启动、停止过（状态或运行代际变了）就重新算。程序正在退出、这台服务器被删掉或换成了同名的
// 另一台，都不再等。
func (m *Manager) waitSettled(i *Instance) bool {
	start := settleNow()
	var since time.Time
	lastState, lastGen := "", -1
	for {
		if m.closing.Load() {
			return false
		}
		if cur, err := m.Get(i.Name); err != nil || cur != i {
			return false
		}
		i.procMu.Lock()
		st, gen := i.state, int(i.runGen)
		i.procMu.Unlock()
		if st == "" {
			st = "stopped"
		}
		now := settleNow()
		if st != lastState || gen != lastGen {
			lastState, lastGen, since = st, gen, now
		}
		if (st == "running" || st == "stopped") && now.Sub(since) >= backupSettleQuiet {
			return true
		}
		if now.Sub(start) >= backupSettleWait {
			return false
		}
		settleSleep(backupSettlePoll)
	}
}

func (m *Manager) execSchedule(i *Instance, s Schedule) {
	start := time.Now()
	note := func(op string, args map[string]any, err error) {
		args["inst"] = i.Name
		if every := s.Interval(); every > 0 { // 操作记录里写清是哪一条：每天几点的，还是每隔多久的
			args["every"] = every
		} else {
			args["at"] = s.At
		}
		m.noteAuto(AutoAction{Op: op, Actor: "定时任务", Args: args, Err: err, Dur: time.Since(start)})
	}
	switch s.Type {
	case "command":
		if i.Status() == "running" {
			err := m.Command(i.Name, s.Args)
			note("instance.command", map[string]any{"cmd": s.Args}, err)
		}
	case "backup":
		_, err := m.createScheduledBackup(i.Name)
		// 撞上服务器启动、停止或重启的，等它稳定下来再试，最多再试两次。
		// 不试的话：备份和重启定在同一分钟（世界大到一分钟压不完，压到一半服务器被重启，这份作废），
		// 或者备份排在重启后一两分钟（服务器还在启动，直接被拒），这一天就没有备份，而且天天如此，
		// 只在控制台留一行字。这两道闸是 2026-11-14 加的，之前这两种情况都照常出一份备份。
		// 再试两次：第一次重试可能正好落在重启中间「已停止」的那一下，随后服务器启动，又被作废一次。
		var unsettled *errBackupUnsettled
		for try := 0; try < 2 && errors.As(err, &unsettled); try++ {
			i.Console.Append("[MineDeck] 定时备份这一次没有做成（" + unsettled.msg + "），等服务器启动或停止完再试")
			if !m.waitSettled(i) {
				break
			}
			_, err = m.createScheduledBackup(i.Name)
		}
		if err != nil {
			i.Console.Append("[MineDeck] 定时备份失败: " + err.Error())
		} else {
			i.Console.Append("[MineDeck] 定时备份完成")
		}
		note("instance.backup.create", map[string]any{"label": scheduledLabel}, err)
	case "restart":
		if i.Status() != "running" {
			return
		}
		i.Console.Append("[MineDeck] 定时重启：60 秒后重启服务器")
		_ = m.Command(i.Name, "say §c服务器将在 60 秒后定时重启，请注意安全下线！")
		time.Sleep(50 * time.Second)
		_ = m.Command(i.Name, "say §c10 秒后重启！")
		time.Sleep(10 * time.Second)
		if err := m.Stop(i.Name); err != nil {
			note("instance.restart", map[string]any{}, err)
			return
		}
		for k := 0; k < 80; k++ { // 最多等 40 秒
			if i.Status() == "stopped" {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		err := fmt.Errorf("等了 40 秒服务器还没停下来，没有重新启动")
		if i.Status() == "stopped" {
			m.waitPortFree(i) // 进程刚退出，端口万一还没放开就等几秒
			if err = m.Start(i.Name); err != nil {
				i.Console.Append("[MineDeck] 定时重启失败: " + err.Error() + restartGaveUp(err))
			}
		}
		note("instance.restart", map[string]any{}, err)
	}
}
