package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
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

// sessionValue 局域网登录会话值（每次启动随机，重启后需重新登录）。
var sessionValue = func() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}()

func validSession(r *http.Request) bool {
	c, err := r.Cookie("amh_session")
	return err == nil && c.Value == sessionValue
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
		Name: "amh_session", Value: sessionValue,
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
