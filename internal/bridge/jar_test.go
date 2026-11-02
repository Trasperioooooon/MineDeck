package bridge

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 内置的两个 jar 和登记的 sha256 一致；jar 里有两份 toml、没有 SVC 的文件，class 都是 Java 8（版本号 52），
// 版本号和 JarVersion 一致。
func TestEmbeddedJars(t *testing.T) {
	for _, v := range []Variant{VariantMain, VariantFallback} {
		b, err := Jar(v)
		if err != nil {
			t.Fatalf("%s：%v", v, err)
		}
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		classes := 0
		for _, f := range zr.File {
			names[f.Name] = true
			if strings.HasPrefix(f.Name, "de/maxhenkel/") {
				t.Errorf("%s 里有 SVC 的文件：%s", v, f.Name)
			}
			if strings.HasSuffix(f.Name, ".class") {
				classes++
				rc, _ := f.Open()
				head := make([]byte, 8)
				_, _ = rc.Read(head)
				rc.Close()
				if head[6] != 0 || head[7] != 52 {
					t.Errorf("%s 的 %s 不是 Java 8 字节码：% x", v, f.Name, head)
				}
			}
			if f.Name == "META-INF/mods.toml" || f.Name == "META-INF/neoforge.mods.toml" {
				rc, _ := f.Open()
				var buf bytes.Buffer
				_, _ = buf.ReadFrom(rc)
				rc.Close()
				if !strings.Contains(buf.String(), `version="`+JarVersion+`"`) {
					t.Errorf("%s 的 %s 版本号不是 %s", v, f.Name, JarVersion)
				}
			}
		}
		for _, need := range []string{"META-INF/mods.toml", "META-INF/neoforge.mods.toml", "pack.mcmeta", "minedeck/voicebridge/BridgePlugin.class"} {
			if !names[need] {
				t.Errorf("%s 里缺 %s", v, need)
			}
		}
		if classes < 10 {
			t.Errorf("%s 只有 %d 个 class", v, classes)
		}
	}
	if _, err := Jar("bogus"); err == nil {
		t.Error("未知的加载方式应报错")
	}
}

// 安装：写进 mods 后逐字节和内置的一样；删掉旧版本和另一种加载方式（同一个模组装两份服务器起不来），
// 别人的模组不动；再装一次什么都不变；换成备用加载方式时删掉默认的那个。sha256 对不上的不装。
func TestInstallJar(t *testing.T) {
	mods := filepath.Join(t.TempDir(), "mods")
	st, err := InspectJar(mods)
	if err != nil || st.Installed || st.Version != JarVersion {
		t.Fatalf("mods 不存在时：%+v %v", st, err)
	}
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"minedeck-voicebridge.jar":                "原型阶段的 0.1.0",
		"minedeck-voicebridge-0.1.0.jar":          "旧版",
		"voicechat-neoforge-1.21.1-2.6.27.jar":    "SVC",
		"MineDeck-VoiceBridge-0.2.0.jar.disabled": "被用户停用的，不是 .jar",
	} {
		if err := os.WriteFile(filepath.Join(mods, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, err = InspectJar(mods)
	if err != nil || !st.Installed || st.Current || st.File != "" || !slices.Equal(st.Others, []string{"minedeck-voicebridge-0.1.0.jar", "minedeck-voicebridge.jar"}) {
		t.Fatalf("装新版前：%+v %v", st, err)
	}
	st, removed, err := InstallJar(mods, VariantMain)
	if err != nil || !st.Current || st.File != "minedeck-voicebridge-"+JarVersion+".jar" || st.Variant != VariantMain || len(removed) != 2 {
		t.Fatalf("安装：%+v %v %v", st, removed, err)
	}
	want, _ := Jar(VariantMain)
	if got, _ := os.ReadFile(filepath.Join(mods, st.File)); !bytes.Equal(got, want) {
		t.Fatal("装进去的和内置的不一样")
	}
	ents, _ := os.ReadDir(mods)
	var left []string
	for _, e := range ents {
		left = append(left, e.Name())
	}
	if !slices.Equal(left, []string{"MineDeck-VoiceBridge-0.2.0.jar.disabled", st.File, "voicechat-neoforge-1.21.1-2.6.27.jar"}) {
		t.Fatalf("mods 里剩下：%v", left)
	}
	if st2, removed, err := InstallJar(mods, VariantMain); err != nil || len(removed) != 0 || !st2.Current {
		t.Fatalf("再装一次：%+v %v %v", st2, removed, err)
	}

	// 有人改了装进去的文件：不算当前版本，重装时换回内置的
	if err := os.WriteFile(filepath.Join(mods, st.File), []byte("改坏了"), 0o644); err != nil {
		t.Fatal(err)
	}
	if st3, _ := InspectJar(mods); st3.Current || st3.File != "" || !slices.Equal(st3.Others, []string{st.File}) {
		t.Fatalf("文件被改过：%+v", st3)
	}
	if st3, _, err := InstallJar(mods, VariantMain); err != nil || !st3.Current {
		t.Fatalf("重装：%+v %v", st3, err)
	}

	// 换成备用加载方式
	st4, removed, err := InstallJar(mods, VariantFallback)
	if err != nil || !st4.Current || st4.Variant != VariantFallback || st4.File != "minedeck-voicebridge-javafml-"+JarVersion+".jar" ||
		!slices.Equal(removed, []string{st.File}) {
		t.Fatalf("换备用：%+v %v %v", st4, removed, err)
	}

	// sha256 对不上：不装，也不留临时文件
	empty := filepath.Join(t.TempDir(), "mods")
	if _, _, err := installJar(empty, "x.jar", []byte("坏的"), jarMainSHA256); err == nil {
		t.Fatal("校验不过应报错")
	}
	if ents, _ := os.ReadDir(empty); len(ents) != 0 {
		t.Fatalf("校验不过还写了文件：%v", ents)
	}
}
