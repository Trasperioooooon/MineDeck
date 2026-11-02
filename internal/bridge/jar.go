package bridge

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 打进 exe 的桥接模组：bridge/voicechat 用 build.sh --fallback 构建的两个 jar，原样入库在 jar/ 下。
// 构建是可复现的（固定时间戳、文件按顺序打包、Created-By 自己写），同样的源码和 JDK 21 构建出的 jar 逐字节相同；
// 重新构建并更新这里用 bridge/voicechat/update-embedded.sh，检查入库的是不是最新的用 update-embedded.sh --check。
// jar 里只有我们自己的代码，不含 SVC 的任何文件（SVC API 只在编译时引用）。
//
//go:embed jar/minedeck-voicebridge.jar jar/minedeck-voicebridge-javafml.jar
var jarFS embed.FS

const (
	// JarVersion 内置模组的版本（和 bridge/voicechat 的 mod_version、BridgePlugin.VERSION 一致）。
	JarVersion = "0.2.0"

	jarMainSHA256     = "11a4e360f1ab879421e17c2f4fad921d5e5b7ef20d0f691c45d5c5f57e891e69"
	jarFallbackSHA256 = "b1af78b47de389abf0e7311138eacd192a1267bb407e84d74621967107e28db3"

	jarPrefix = "minedeck-voicebridge"
)

// Variant 模组的加载方式。
type Variant string

const (
	VariantMain     Variant = "main"    // lowcodefml：没有入口类，SVC 自己扫描注解找到插件（默认）
	VariantFallback Variant = "javafml" // 备用：javafml + 两个空入口类，默认的在真服上找不到插件时换这个
)

// jarFile 一个加载方式对应的内置文件和装进 mods 时的文件名。
func jarFile(v Variant) (embedded, installed, sum string, err error) {
	switch v {
	case VariantMain, "":
		return "jar/minedeck-voicebridge.jar", jarPrefix + "-" + JarVersion + ".jar", jarMainSHA256, nil
	case VariantFallback:
		return "jar/minedeck-voicebridge-javafml.jar", jarPrefix + "-javafml-" + JarVersion + ".jar", jarFallbackSHA256, nil
	}
	return "", "", "", fmt.Errorf("未知的加载方式 %q", v)
}

// JarName 装进 mods 的文件名。
func JarName(v Variant) string {
	_, name, _, _ := jarFile(v)
	return name
}

// Jar 内置 jar 的内容，校验过 sha256（exe 被改坏了会报错，不会把坏文件装进服务器）。
func Jar(v Variant) ([]byte, error) {
	path, _, sum, err := jarFile(v)
	if err != nil {
		return nil, err
	}
	b, err := jarFS.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if sha256Hex(b) != sum {
		return nil, errors.New("内置的桥接模组 jar 校验失败（sha256 对不上），这个 MineDeck 程序可能已损坏，请重新下载")
	}
	return b, nil
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// JarStatus mods 目录里桥接模组的情况。
type JarStatus struct {
	Installed bool     `json:"installed"`         // 有我们的模组（任何版本）
	File      string   `json:"file,omitempty"`    // 和内置版本逐字节相同的那个文件
	Variant   Variant  `json:"variant,omitempty"` // 它的加载方式
	Current   bool     `json:"current"`           // 装的就是内置版本（而且只有这一个）
	Others    []string `json:"others,omitempty"`  // 别的版本（旧版、另一种加载方式、改过的），安装时会删掉
	Version   string   `json:"version"`           // 内置版本
}

// isOurs 文件名像是我们的模组（各版本、两种加载方式；原型阶段的名字不带版本号）。
func isOurs(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, jarPrefix) && strings.HasSuffix(n, ".jar")
}

// InspectJar 看 mods 目录里装的是什么。mods 目录不存在时什么都没装。
func InspectJar(modsDir string) (JarStatus, error) {
	st := JarStatus{Version: JarVersion}
	ents, err := os.ReadDir(modsDir)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	for _, e := range ents {
		if e.IsDir() || !isOurs(e.Name()) {
			continue
		}
		st.Installed = true
		matched := false
		for _, v := range []Variant{VariantMain, VariantFallback} {
			_, name, sum, _ := jarFile(v)
			if e.Name() != name || st.File != "" {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(modsDir, e.Name())); err == nil && sha256Hex(b) == sum {
				st.File, st.Variant, matched = e.Name(), v, true
			}
		}
		if !matched {
			st.Others = append(st.Others, e.Name())
		}
	}
	sort.Strings(st.Others)
	st.Current = st.File != "" && len(st.Others) == 0
	return st, nil
}

// InstallJar 把内置的 jar 装进 mods：先写临时文件、读回来核对 sha256，再改名；然后删掉别的版本
// （同一个模组装两份，加载器会报重复模组、服务器起不来）。返回装好后的情况和删掉的文件。
// 服务器运行中时，加载器占着旧 jar，Windows 上删不掉；调用方应在停服后再替换旧版本。
func InstallJar(modsDir string, v Variant) (JarStatus, []string, error) {
	data, err := Jar(v)
	if err != nil {
		return JarStatus{}, nil, err
	}
	_, name, sum, _ := jarFile(v)
	return installJar(modsDir, name, data, sum)
}

func installJar(modsDir, name string, data []byte, sum string) (JarStatus, []string, error) {
	if sha256Hex(data) != sum {
		return JarStatus{}, nil, errors.New("桥接模组 jar 校验失败（sha256 对不上），没有安装")
	}
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return JarStatus{}, nil, err
	}
	dest := filepath.Join(modsDir, name)
	if b, err := os.ReadFile(dest); err != nil || !bytes.Equal(b, data) {
		tmp := filepath.Join(modsDir, "."+name+".tmp") // 不以 .jar 结尾：加载器不会把它当模组
		if err := os.WriteFile(tmp, data, 0o644); err != nil {
			return JarStatus{}, nil, err
		}
		if b, err := os.ReadFile(tmp); err != nil || sha256Hex(b) != sum {
			os.Remove(tmp)
			return JarStatus{}, nil, errors.New("写进 mods 的桥接模组校验失败，没有安装（磁盘可能有问题）")
		}
		if err := os.Rename(tmp, dest); err != nil {
			os.Remove(tmp)
			return JarStatus{}, nil, fmt.Errorf("安装桥接模组失败（%s 可能正被服务器占用，停服后再试）：%w", name, err)
		}
	}
	ents, err := os.ReadDir(modsDir)
	if err != nil {
		return JarStatus{}, nil, err
	}
	var removed []string
	var errs []error
	for _, e := range ents {
		if e.IsDir() || !isOurs(e.Name()) || e.Name() == name {
			continue
		}
		if err := os.Remove(filepath.Join(modsDir, e.Name())); err != nil {
			errs = append(errs, fmt.Errorf("删不掉旧版本 %s（可能正被服务器占用，停服后再试）：%w", e.Name(), err))
			continue
		}
		removed = append(removed, e.Name())
	}
	st, err := InspectJar(modsDir)
	if err == nil {
		err = errors.Join(errs...)
	}
	return st, removed, err
}
