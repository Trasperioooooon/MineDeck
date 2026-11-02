package modinfo

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeJar 在 dir 里写一个 jar，files 是 jar 里的文件名 → 内容。
func writeJar(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for n, body := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

const neoTOML = `modLoader="javafml" #mandatory
loaderVersion="[4,)"
license="MIT"
[[mods]]
modId="create" #mandatory
version="6.0.4"
displayName="Create"
description='''
Building Tools and Aesthetic Technology
多行描述 "引号" 也没关系
'''
[[mixins]]
config="create.mixins.json"
[[dependencies.create]]
    modId="neoforge"
    type="required"
    versionRange="[21.1.77,)"
    ordering="NONE"
    side="BOTH"
[[dependencies.create]]
    modId="flywheel"
    type="required"
    versionRange="[1.0.0,2.0)"
    side="CLIENT"
[[dependencies.create]]
    modId = "optifine"
    type = "incompatible"
    reason = "渲染冲突"
`

const forgeTOML = `modLoader="javafml"
loaderVersion="[47,)"
[[mods]]
modId="mowziesmobs"
version="${file.jarVersion}"
displayName="Mowzie's Mobs"
[[mods]]
modId="mowzies_extra"
[[dependencies."mowziesmobs"]]
modId="geckolib"
mandatory=true
versionRange="[4.2,)"
side="BOTH"
[[dependencies.mowziesmobs]]
modId="jei"
mandatory=false
versionRange="*"
`

func TestReadDir(t *testing.T) {
	dir := t.TempDir()
	writeJar(t, dir, "create-1.21.1-6.0.4.jar", map[string]string{"META-INF/neoforge.mods.toml": neoTOML,
		"com/simibubi/create/Create.class": "x"})
	writeJar(t, dir, "mowziesmobs-1.6.4.jar", map[string]string{"META-INF/mods.toml": forgeTOML,
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nImplementation-Version: 1.6.4\r\nMixinConfigs: mowziesmobs.mixins.j\r\n son,extra.mixins.json\r\n"})
	writeJar(t, dir, "sodium-fabric.jar", map[string]string{"fabric.mod.json": `{"schemaVersion":1,"id":"sodium","version":"0.6.0","name":"Sodium",
		"environment":"client","depends":{"minecraft":["1.21","1.21.1"],"fabricloader":">=0.16"},"breaks":{"optifabric":"*"},
		"mixins":["sodium.mixins.json",{"config":"sodium-extra.mixins.json","environment":"client"}]}`})
	writeJar(t, dir, "EssentialsX-2.21.jar", map[string]string{"plugin.yml": "name: Essentials\nversion: '2.21.0'\ndepend: [Vault]\nsoftdepend:\n  - LuckPerms\n  - \"PlaceholderAPI\"\nmain: com.earth2me.essentials.Essentials\n"})
	writeJar(t, dir, "bridge.jar", map[string]string{"META-INF/neoforge.mods.toml": "[[mods]]\nmodId=\"bridge\"\n", "META-INF/mods.toml": "[[mods]]\nmodId=\"bridge\"\n"})
	writeJar(t, dir, "old.jar.disabled", map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"old\"\nversion=\"1.0\"\n"})
	writeJar(t, dir, "library.jar", map[string]string{"a/B.class": "x"})
	if err := os.WriteFile(filepath.Join(dir, "broken.jar"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mods, err := ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Mod{}
	var files []string
	for _, m := range mods {
		by[m.File] = m
		files = append(files, m.File)
	}
	if want := []string{"bridge.jar", "broken.jar", "create-1.21.1-6.0.4.jar", "EssentialsX-2.21.jar", "library.jar", "mowziesmobs-1.6.4.jar", "old.jar.disabled", "sodium-fabric.jar"}; !reflect.DeepEqual(files, want) {
		t.Fatalf("文件：%v", files)
	}
	c := by["create-1.21.1-6.0.4.jar"]
	if c.Loader != "neoforge" || c.ID != "create" || c.Version != "6.0.4" || c.Name != "Create" || c.Err != "" ||
		!reflect.DeepEqual(c.Mixins, []string{"create.mixins.json"}) {
		t.Errorf("NeoForge 模组：%+v", c)
	}
	if want := []Dep{{ID: "neoforge", Range: "[21.1.77,)", Required: true, Side: "BOTH"}, {ID: "flywheel", Range: "[1.0.0,2.0)", Required: true, Side: "CLIENT"},
		{ID: "optifine", Kind: "incompatible"}}; !reflect.DeepEqual(c.Deps, want) {
		t.Errorf("NeoForge 依赖：%+v", c.Deps)
	}
	m := by["mowziesmobs-1.6.4.jar"]
	if m.Loader != "forge" || m.ID != "mowziesmobs" || m.Version != "1.6.4" || !reflect.DeepEqual(m.Also, []string{"mowzies_extra"}) ||
		!reflect.DeepEqual(m.Mixins, []string{"mowziesmobs.mixins.json", "extra.mixins.json"}) {
		t.Errorf("Forge 模组（版本取自 MANIFEST，MixinConfigs 有续行）：%+v", m)
	}
	if want := []Dep{{ID: "geckolib", Range: "[4.2,)", Required: true, Side: "BOTH"}, {ID: "jei", Range: "*"}}; !reflect.DeepEqual(m.Deps, want) {
		t.Errorf("Forge 依赖：%+v", m.Deps)
	}
	s := by["sodium-fabric.jar"]
	if s.Loader != "fabric" || s.ID != "sodium" || s.Env != "client" || !reflect.DeepEqual(s.Mixins, []string{"sodium.mixins.json", "sodium-extra.mixins.json"}) {
		t.Errorf("Fabric 模组：%+v", s)
	}
	if want := []Dep{{ID: "fabricloader", Range: ">=0.16", Required: true}, {ID: "minecraft", Range: "1.21 || 1.21.1", Required: true}, {ID: "optifabric", Range: "*", Kind: "incompatible"}}; !reflect.DeepEqual(s.Deps, want) {
		t.Errorf("Fabric 依赖：%+v", s.Deps)
	}
	e := by["EssentialsX-2.21.jar"]
	if e.Loader != "plugin" || e.ID != "Essentials" || e.Version != "2.21.0" ||
		!reflect.DeepEqual(e.Deps, []Dep{{ID: "Vault", Required: true}, {ID: "LuckPerms"}, {ID: "PlaceholderAPI"}}) {
		t.Errorf("插件：%+v", e)
	}
	if b := by["bridge.jar"]; b.Loader != "neoforge+forge" || b.ID != "bridge" {
		t.Errorf("两种描述文件都有：%+v", b)
	}
	if o := by["old.jar.disabled"]; !o.Disabled || o.ID != "old" {
		t.Errorf("禁用的模组：%+v", o)
	}
	if by["broken.jar"].Err == "" || by["library.jar"].Err == "" || by["library.jar"].Loader != "unknown" {
		t.Errorf("坏的 jar、不是模组的 jar 要写明原因：%+v %+v", by["broken.jar"], by["library.jar"])
	}
	// 缓存：文件没变就不重读；变了重读
	writeJar(t, dir, "old.jar.disabled", map[string]string{"META-INF/mods.toml": "[[mods]]\nmodId=\"old\"\nversion=\"2.0\"\n"})
	mods, _ = ReadDir(dir)
	for _, x := range mods {
		if x.File == "old.jar.disabled" && x.Version != "2.0" {
			t.Errorf("文件变了应重读：%+v", x)
		}
	}
	if l, err := ReadDir(filepath.Join(dir, "none")); err != nil || len(l) != 0 {
		t.Errorf("目录不存在：%v %v", l, err)
	}
	if FindClass(dir, "com.simibubi.create.Create") != "create-1.21.1-6.0.4.jar" || FindClass(dir, "a/B") != "library.jar" || FindClass(dir, "x/Y") != "" {
		t.Error("按类名找 jar")
	}
}
