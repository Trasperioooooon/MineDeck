package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
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

// handleLogin 局域网访问的密码登录。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	hash := app.GetConfig().AccessPasswordHash
	if hash == "" {
		writeErr(w, 400, fmt.Errorf("未启用远程访问"))
		return
	}
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
