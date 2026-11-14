package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"minedeck/internal/app"
)

// Token 每次启动随机生成，通过启动 URL 传入前端。
var Token = func() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}()

// hasToken 请求带没带这次启动的令牌（请求头 X-Token，或者查询串 ?token=：EventSource 设不了请求头）。
// 接口的登录检查（secure）和「算不算本机」（callerOf）都用它。
func hasToken(r *http.Request) bool {
	tok := r.Header.Get("X-Token")
	if tok == "" {
		tok = r.URL.Query().Get("token")
	}
	return tok != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(Token)) == 1
}

// 局域网登录的会话值：每次启动随机，重启后要重新登录。
// 关掉过局域网访问、或者换了访问密码，也换一个新的：之前登录过的设备都要重新登录。
// 以前要到重启才换，关掉局域网访问再打开（中间不重启），拿着旧 Cookie 的设备照样能进（独立评审发现）。
var sess = struct {
	sync.Mutex
	value string
	lan   bool   // 上次看到的「允许局域网访问」（开着且设了密码）
	hash  string // 上次看到的访问密码哈希
}{value: newSessionValue()}

func newSessionValue() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// syncSession 对照现在的配置：局域网访问从开到关、或者访问密码换了，就换会话值。返回现在的会话值。
// 每个请求进来都先调一次（secure），开关的变化总能在下一次请求时看到，不管配置是从哪条路改的。
func syncSession(cfg app.Config) string {
	lan := cfg.ListenLAN && cfg.AccessPasswordHash != ""
	sess.Lock()
	defer sess.Unlock()
	if (sess.lan && !lan) || (sess.hash != "" && cfg.AccessPasswordHash != sess.hash) {
		sess.value = newSessionValue()
	}
	sess.lan, sess.hash = lan, cfg.AccessPasswordHash
	return sess.value
}

func validSession(r *http.Request, current string) bool {
	c, err := r.Cookie("amh_session")
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(current)) == 1
}

// loginMaxBody 登录请求里只有一个密码，几 KB 足够。以前整个请求体读完才校验，
// 没登录的人发几 MB 的 JSON 也会被整份读进内存（v3.1.0 正式版前检查 B-1）。
const loginMaxBody = 4 << 10

// 校验密码时的并发上限：同一个来源同时只校验一个，全部来源加起来最多 loginMaxConcurrent 个。
// bcrypt 每次要几十毫秒 CPU，猜错还要等 0.7 秒；以前同时发很多请求，这个等待就形同虚设（B-1）。
// 全局上限是给换着 IPv6 地址来的情况兜底。
const loginMaxConcurrent = 8

var loginGate = struct {
	sync.Mutex
	busy  map[string]bool // 正在校验的来源
	count int
}{busy: map[string]bool{}}

// acquireLogin 占一个校验名额；占不到（这个来源正在校验，或者总数满了）返回 false。
func acquireLogin(r *http.Request) (release func(), ok bool) {
	src := r.RemoteAddr
	if h, _, err := net.SplitHostPort(src); err == nil {
		src = h
	}
	loginGate.Lock()
	defer loginGate.Unlock()
	if loginGate.busy[src] || loginGate.count >= loginMaxConcurrent {
		return nil, false
	}
	loginGate.busy[src] = true
	loginGate.count++
	return func() {
		loginGate.Lock()
		delete(loginGate.busy, src)
		loginGate.count--
		loginGate.Unlock()
	}, true
}

// handleLogin 局域网访问的密码登录。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, loginMaxBody)
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Errorf("请求太大"))
			return
		}
		writeErr(w, 400, err)
		return
	}
	hash := app.GetConfig().AccessPasswordHash
	if hash == "" {
		writeErr(w, 400, fmt.Errorf("未启用远程访问"))
		return
	}
	// 读完请求体再占名额：慢慢发请求体的连接不会一直占着名额
	release, ok := acquireLogin(r)
	if !ok {
		writeErr(w, http.StatusTooManyRequests, fmt.Errorf("请求太频繁，稍等几秒再试"))
		return
	}
	defer release()
	if !checkPassword(hash, body.Password) {
		time.Sleep(700 * time.Millisecond) // 抑制暴力尝试
		writeErr(w, 401, fmt.Errorf("密码错误"))
		return
	}
	// 旧版无盐 SHA-256 哈希登录成功后就地升级为 bcrypt（哈希被并发改过则不动）
	if !strings.HasPrefix(hash, "$2") {
		if h, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost); err == nil {
			_, _ = app.UpdateConfig(func(c *app.Config) error {
				if c.AccessPasswordHash == hash {
					c.AccessPasswordHash = string(h)
				}
				return nil
			})
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name: "amh_session", Value: syncSession(app.GetConfig()), // 上面就地升级过密码哈希时，这里先换会话值再发，发出去的就是新的
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, "ok")
}

// checkPassword 校验远程访问密码：bcrypt 为主；兼容 v2.3 及更早的无盐
// SHA-256 hex（登录成功后由调用方就地升级，无需用户重设密码）。
func checkPassword(hash, password string) bool {
	if strings.HasPrefix(hash, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	sum := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(hash)) == 1
}
