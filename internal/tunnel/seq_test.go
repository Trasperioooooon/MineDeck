package tunnel

import (
	"os"
	"testing"
)

// tunnels.json 里的 seq 比现有编号小时，load 只在内存里把 seq 抬到现有编号的最大值，不为它单独写回文件，
// 下次新增隧道不会拿到已经用过的编号。
func TestLoadRaisesSeqToMaxExistingID(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantID  string
		newPort int
	}{
		{
			name:    "seq小于现有最大编号",
			json:    `{"seq":1,"tunnels":[{"id":"tun1","provider":"custom","name":"自建一","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565},{"id":"tun5","provider":"custom","name":"自建五","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25567,"localPort":25565},{"id":"custom","provider":"custom","name":"自定义名","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25568,"localPort":25565}]}`,
			wantID:  "tun6",
			newPort: 25569,
		},
		{
			name:    "seq更大时照用文件里的seq",
			json:    `{"seq":10,"tunnels":[{"id":"tun3","provider":"custom","name":"自建三","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565}]}`,
			wantID:  "tun11",
			newPort: 25567,
		},
		{
			name:    "带前导零的编号照常解析",
			json:    `{"seq":0,"tunnels":[{"id":"tun007","provider":"custom","name":"自建零零七","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565}]}`,
			wantID:  "tun8",
			newPort: 25567,
		},
		{
			name:    "没有隧道时从tun1开始",
			json:    `{"seq":0,"tunnels":[]}`,
			wantID:  "tun1",
			newPort: 25566,
		},
		{
			name:    "忽略不符合格式或溢出的编号",
			json:    `{"seq":2,"tunnels":[{"id":"tun","provider":"custom","name":"空数字","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25566,"localPort":25565},{"id":"tun-3","provider":"custom","name":"带负号","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25567,"localPort":25565},{"id":"tun 7","provider":"custom","name":"带空格","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25568,"localPort":25565},{"id":"tun+9","provider":"custom","name":"带加号","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25569,"localPort":25565},{"id":"abc","provider":"custom","name":"非tun前缀","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25570,"localPort":25565},{"id":"tun99999999999999999999999","provider":"custom","name":"溢出","serverAddr":"1.2.3.4","serverPort":7000,"remotePort":25571,"localPort":25565}]}`,
			wantID:  "tun3",
			newPort: 25572,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempBase(t)
			if err := os.WriteFile(tunnelsPath(), []byte(tc.json), 0o600); err != nil {
				t.Fatal(err)
			}
			m := NewManager()
			if err := m.LoadErr(); err != nil {
				t.Fatalf("load 失败：%v", err)
			}
			if got := readFile(t, tunnelsPath()); got != tc.json {
				t.Fatalf("load 之后文件内容被动了：\n前：%s\n后：%s", tc.json, got)
			}
			added, err := m.Add(Tunnel{
				Provider:   "custom",
				Name:       "新增隧道",
				ServerAddr: "1.2.3.4",
				ServerPort: 7000,
				RemotePort: tc.newPort,
				LocalPort:  25565,
			})
			if err != nil {
				t.Fatalf("Add 失败：%v", err)
			}
			if added.ID != tc.wantID {
				t.Fatalf("新隧道编号 = %q，期望 %q", added.ID, tc.wantID)
			}
		})
	}
}
