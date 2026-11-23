package worldimport

import "sync"

// 正在跑的换地图任务（导入地图、换回导入前的地图）。世界状态里带上它的编号，别的设备打开同一台服务器也接得上进度；
// 顺带说清问的人现在能不能取消、不能的话为什么，界面据此把「取消导入」变灰并写出原因（A53）。

// WorldTask 的 Kind。
const (
	jobImport  = "import"
	jobRestore = "restore"
)

// WorldTask 这台服务器上正在跑的换地图任务（WorldStatus.Task）。
type WorldTask struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // import（导入地图）| restore（换回导入前的地图）
	// CanCancel 问的人现在点「取消导入」能不能取消掉；不能时 CancelWhy 说为什么。换回旧地图一直不能取消
	CanCancel bool   `json:"canCancel"`
	CancelWhy string `json:"cancelWhy,omitempty"`
}

type worldJob struct {
	inst string
	kind string
	seq  int    // 登记的先后：同一台服务器上同时有两个时取后登记的
	why  string // 已经过了能取消的阶段时给人看的原因；导入还在复制时为空
}

// 过了能取消的阶段以后给人看的原因。导入的两句由 closeCancel、换地图那一步记下，换回旧地图登记时就记上。
const (
	whyStopping  = "已经开始停服、准备换上新地图，不能取消了：会换完，出错就自动退回原来的世界"
	whyReplacing = "正在替换世界文件夹，这一步不能停：换完或者自动退回原来的世界以后才会结束"
	whyRestoring = "正在换回导入前的地图，这一步不能停：换完或者自动退回以后才会结束"
	whyNotOwner  = "只有发起这次导入的那台设备能取消"
	whyCanceling = "已经在取消了，正在清掉复制了一半的地图"
	whyGone      = "这个任务已经不能取消了"
)

// addJob 登记一个刚建好的换地图任务，返回撤销登记的函数（撤一次就够，多调没关系）。
// 调用方在报任务结果之前撤：任务一显示结束，世界状态里就不再带着它，界面点「完成」回到首页时不会又被接回进度页。
func (s *Service) addJob(inst, taskID, kind, why string) func() {
	s.mu.Lock()
	s.jobSeq++
	s.jobs[taskID] = &worldJob{inst: inst, kind: kind, seq: s.jobSeq, why: why}
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.jobs, taskID)
			s.mu.Unlock()
		})
	}
}

// setJobWhyLocked 记下这个任务之后被问「能不能取消」时给的原因。调用方拿着 s.mu。
func (s *Service) setJobWhyLocked(taskID, why string) {
	if j := s.jobs[taskID]; j != nil {
		j.why = why
	}
}

func (s *Service) setJobWhy(taskID, why string) {
	s.mu.Lock()
	s.setJobWhyLocked(taskID, why)
	s.mu.Unlock()
}

// TaskOf 这台服务器上正在跑的换地图任务，没有返回 nil。owner 是问的人（同 Execute、Cancel）：只有发起导入的人能取消。
func (s *Service) TaskOf(owner, name string) *WorldTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	var job *worldJob
	for k, j := range s.jobs {
		if j.inst == name && (job == nil || j.seq > job.seq) {
			id, job = k, j
		}
	}
	if job == nil {
		return nil
	}
	wt := &WorldTask{ID: id, Kind: job.kind}
	wt.CanCancel, wt.CancelWhy = s.cancelStateLocked(owner, id)
	return wt
}

// CancelRefusal Cancel 没收下时给人看的原因。
func (s *Service) CancelRefusal(owner, taskID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok, why := s.cancelStateLocked(owner, taskID); !ok {
		return why
	}
	return whyGone // 走不到：Cancel 拒了以后，取消的登记只会被拿掉，不会再回来
}

// cancelStateLocked 问的人现在能不能取消这个任务、不能的话为什么。能不能以取消的登记（s.cancels）为准，和 Cancel 一样，
// 不另外按步骤猜。调用方拿着 s.mu（锁的先后同 Cancel：先 s.mu，后任务自己的锁）。
func (s *Service) cancelStateLocked(owner, taskID string) (bool, string) {
	c, ok := s.cancels[taskID]
	if !ok {
		if j := s.jobs[taskID]; j != nil && j.why != "" {
			return false, j.why
		}
		return false, whyGone
	}
	if c.owner != owner {
		return false, whyNotOwner
	}
	if s.M != nil && s.M.Tasks != nil {
		if t := s.M.Tasks.Get(taskID); t != nil && t.Cancelled() {
			return false, whyCanceling
		}
	}
	return true, ""
}
