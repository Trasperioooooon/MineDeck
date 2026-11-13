package winname

import "testing"

// 保留设备名不看这台 Windows 把它当不当设备：带扩展名的、上标数字的、大小写怎么写都算（v3.1.0 正式版前检查 C-2e）。
func TestReserved(t *testing.T) {
	for _, bad := range []string{
		"CON", "con", "Con.txt", "CON.tar.gz", "NUL .txt", "nul ", "PRN.log", "aux.json",
		"COM1", "com1.txt", "COM0.jar", "LPT9.cfg", "lpt1", "COM¹", "com².txt", "LPT³.jar",
		"CONIN$", "conout$.txt",
	} {
		if !Reserved(bad) {
			t.Errorf("%q 是保留设备名", bad)
		}
	}
	for _, ok := range []string{
		"CONFIG", "console.log", "CON1.txt", "COM10", "COM1x.txt", "LPT", "COMA.txt", "nul_x.txt", "xCON.txt",
		"CON-fig.toml", "AUX_data", "COM⁴.txt", "", ".con",
	} {
		if Reserved(ok) {
			t.Errorf("%q 不是保留设备名", ok)
		}
	}
}

func TestOKAndPathOK(t *testing.T) {
	for _, bad := range []string{
		"", ".", "..", "...", "a.txt.", "a.txt ", "a. ", " ", "run.bat.", "CON.txt", "com1.log", "LPT¹",
		"a:b", "a*b", "a?b", `a"b`, "a<b", "a>b", "a|b", `a\b`, "a/b", "a\x00b", "a\nb", "a\x1fb",
	} {
		if OK(bad) {
			t.Errorf("%q 不能原样当文件名", bad)
		}
	}
	for _, ok := range []string{
		"a.txt", ".minedeck", "server.properties", "Faithful 32x.zip", "中文 名字.toml", "a..b", " lead.txt", "a.b.c",
		"CONFIG", "console.log", "a\x7fb",
	} {
		if !OK(ok) {
			t.Errorf("%q 是能用的名字", ok)
		}
	}
	for _, bad := range []string{
		"", "/abs.txt", "a//b.txt", "config/../x", "./x", "config/CON.txt", "world/COM1", "mods ./a.jar", "a/b.",
		"config/x.toml ", "C:/x.txt",
	} {
		if PathOK(bad) {
			t.Errorf("%q 不能原样当路径", bad)
		}
	}
	for _, ok := range []string{"config/x.toml", "world/region/r.0.0.mca", "resourcepacks/Faithful 32x.zip", "a"} {
		if !PathOK(ok) {
			t.Errorf("%q 是能用的路径", ok)
		}
	}
}

func TestFold(t *testing.T) {
	if Fold("Config/A.toml") != Fold(`config\a.TOML`) || Fold("config/a.toml") == Fold("config/b.toml") {
		t.Error("只差大小写和斜杠方向的应是同一个键，名字不同的不是")
	}
}
