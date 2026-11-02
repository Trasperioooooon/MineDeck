package inst

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"minedeck/internal/dl"
	"minedeck/internal/events"
	"minedeck/internal/mclog"
)

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

type PlayerEntry struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

type PlayerLists struct {
	Online    []string      `json:"online"`
	Whitelist []PlayerEntry `json:"whitelist"`
	Ops       []PlayerEntry `json:"ops"`
	Banned    []PlayerEntry `json:"banned"`
}

// trackPlayers 从控制台输出维护在线玩家集合。识别规则在 mclog：只对拆出前缀后的正文做锚定匹配，
// 玩家在聊天里打一句「Notch joined the game」不会把 Notch 加进在线列表。
// 一次进服会有「logged in with entity id」和「joined the game」两行（退服同理），只在名单真的变化时推送事件。
func (i *Instance) trackPlayers(line string) {
	l := mclog.Parse(line)
	switch l.Kind {
	case mclog.Join:
		i.onlineMu.Lock()
		if i.online == nil {
			i.online = map[string]bool{}
		}
		added := !i.online[l.Player]
		i.online[l.Player] = true
		i.onlineMu.Unlock()
		if added {
			events.Publish("player.join", map[string]any{"instance": i.Name, "player": l.Player})
		}
	case mclog.Leave:
		i.onlineMu.Lock()
		was := i.online[l.Player]
		delete(i.online, l.Player)
		i.onlineMu.Unlock()
		if was {
			events.Publish("player.leave", map[string]any{"instance": i.Name, "player": l.Player})
		}
	}
}

func (i *Instance) clearOnline() {
	i.onlineMu.Lock()
	i.online = map[string]bool{}
	i.onlineMu.Unlock()
}

// GetPlayers 汇总在线玩家与三类名单。
func (m *Manager) GetPlayers(name string) (*PlayerLists, error) {
	i, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	out := &PlayerLists{Online: []string{}, Whitelist: []PlayerEntry{}, Ops: []PlayerEntry{}, Banned: []PlayerEntry{}}
	i.onlineMu.Lock()
	for n := range i.online {
		out.Online = append(out.Online, n)
	}
	i.onlineMu.Unlock()
	sort.Strings(out.Online)
	out.Whitelist = readPlayerFile(filepath.Join(i.Dir, "whitelist.json"))
	out.Ops = readPlayerFile(filepath.Join(i.Dir, "ops.json"))
	out.Banned = readPlayerFile(filepath.Join(i.Dir, "banned-players.json"))
	return out, nil
}

// PlayerAction 执行玩家操作。运行中走控制台命令；停止时直接改名单文件。
func (m *Manager) PlayerAction(name, action, player string) error {
	i, err := m.Get(name)
	if err != nil {
		return err
	}
	if !mclog.ValidName(player) {
		return fmt.Errorf("玩家名不合法（1~16 位字母数字下划线）")
	}
	running := i.Status() == "running"
	cmds := map[string]string{
		"op": "op %s", "deop": "deop %s",
		"whitelist-add": "whitelist add %s", "whitelist-remove": "whitelist remove %s",
		"kick": "kick %s", "ban": "ban %s", "pardon": "pardon %s",
	}
	tpl, ok := cmds[action]
	if !ok {
		return fmt.Errorf("未知操作: %s", action)
	}
	if running {
		return m.Command(name, fmt.Sprintf(tpl, player))
	}
	if action == "kick" {
		return fmt.Errorf("踢出玩家需要服务器运行中")
	}
	// 停止状态：直接编辑名单文件
	uuid, err := m.lookupUUID(i, player)
	if err != nil {
		return err
	}
	switch action {
	case "op":
		return upsertPlayerFile(filepath.Join(i.Dir, "ops.json"), player, map[string]any{
			"uuid": uuid, "name": player, "level": 4, "bypassesPlayerLimit": false,
		})
	case "deop":
		return removeFromPlayerFile(filepath.Join(i.Dir, "ops.json"), player)
	case "whitelist-add":
		return upsertPlayerFile(filepath.Join(i.Dir, "whitelist.json"), player, map[string]any{
			"uuid": uuid, "name": player,
		})
	case "whitelist-remove":
		return removeFromPlayerFile(filepath.Join(i.Dir, "whitelist.json"), player)
	case "ban":
		return upsertPlayerFile(filepath.Join(i.Dir, "banned-players.json"), player, map[string]any{
			"uuid": uuid, "name": player,
			"created": time.Now().Format("2006-01-02 15:04:05 -0700"),
			"source":  "MineDeck", "expires": "forever", "reason": "Banned by an operator.",
		})
	case "pardon":
		return removeFromPlayerFile(filepath.Join(i.Dir, "banned-players.json"), player)
	}
	return nil
}

// lookupUUID 离线模式用确定性 UUID（UUID v3 of "OfflinePlayer:"+名字）；
// 正版模式查询 Mojang API。
func (m *Manager) lookupUUID(i *Instance, player string) (string, error) {
	onlineMode := false
	if p, err := LoadProps(i.PropsPath()); err == nil {
		if v, ok := p.Get("online-mode"); ok {
			onlineMode = v == "true"
		}
	}
	if !onlineMode {
		return offlineUUID(player), nil
	}
	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	ctx, cancel := contextWithTimeout(15 * time.Second)
	defer cancel()
	err := dl.FetchJSON(ctx, []string{"https://api.mojang.com/users/profiles/minecraft/" + player}, &resp)
	if err != nil || len(resp.ID) != 32 {
		return "", fmt.Errorf("查询正版 UUID 失败（可在服务器运行时再操作，将自动走游戏命令）")
	}
	id := resp.ID
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:32], nil
}

// offlineUUID 与官方离线模式算法一致的 UUID v3。
func offlineUUID(player string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + player))
	h[6] = (h[6] & 0x0f) | 0x30 // version 3
	h[8] = (h[8] & 0x3f) | 0x80 // RFC4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// ---------- 名单 JSON 文件读写（保留未知字段） ----------

func readPlayerFile(path string) []PlayerEntry {
	items := readPlayerRaw(path)
	out := make([]PlayerEntry, 0, len(items))
	for _, it := range items {
		e := PlayerEntry{}
		if v, ok := it["name"].(string); ok {
			e.Name = v
		}
		if v, ok := it["uuid"].(string); ok {
			e.UUID = v
		}
		if e.Name != "" {
			out = append(out, e)
		}
	}
	return out
}

func readPlayerRaw(path string) []map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var items []map[string]any
	if json.Unmarshal(b, &items) != nil {
		return nil
	}
	return items
}

func writePlayerRaw(path string, items []map[string]any) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func upsertPlayerFile(path, player string, entry map[string]any) error {
	items := readPlayerRaw(path)
	for _, it := range items {
		if n, ok := it["name"].(string); ok && strings.EqualFold(n, player) {
			return nil // 已存在
		}
	}
	items = append(items, entry)
	return writePlayerRaw(path, items)
}

func removeFromPlayerFile(path, player string) error {
	items := readPlayerRaw(path)
	out := items[:0]
	for _, it := range items {
		if n, ok := it["name"].(string); ok && strings.EqualFold(n, player) {
			continue
		}
		out = append(out, it)
	}
	return writePlayerRaw(path, out)
}
