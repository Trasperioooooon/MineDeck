package toolbox

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxSkinBytes 单张皮肤 PNG 最大体积（2 MiB，正常 64×64 PNG 通常仅几 KB）。
	MaxSkinBytes = 2 << 20
	// MaxSkins 一次打包最多接收的皮肤张数（对齐 1.19.3+ 默认皮肤的 18 个槽位）。
	MaxSkins = 18
	// MaxSkinUploadBytes 整次表单上传总上限（40 MiB）。
	MaxSkinUploadBytes = 40 << 20
)

// SkinSlot 一个可替换的默认皮肤槽位。
type SkinSlot struct {
	Index      int    `json:"index"`
	ID         string `json:"id"`
	Character  string `json:"character"`
	Model      string `json:"model"`      // wide | slim
	ModelLabel string `json:"modelLabel"` // 经典（4px） / 纤细（3px）
	Path       string `json:"path"`       // 资源包 zip 内固定路径
}

// 1.19.3 起 Java 版 DefaultPlayerSkin.DEFAULT_SKINS 的完整 18 槽位顺序（共 9 个角色 × 细/宽两套手臂模型）。
// 出处：net.minecraft.client.resources.DefaultPlayerSkin（Minecraft Java Edition 1.19.3+），
// 前 9 个（0..8）为 slim 按字母序，后 9 个（9..17）为 wide 按字母序，客户端通过 DEFAULT_SKINS[Math.floorMod(uuid.hashCode(), 18)] 选定。
var modernSkinChars = []string{"alex", "ari", "efe", "kai", "makena", "noor", "steve", "sunny", "zuri"}

var charTitles = map[string]string{
	"alex": "Alex", "ari": "Ari", "efe": "Efe", "kai": "Kai", "makena": "Makena",
	"noor": "Noor", "steve": "Steve", "sunny": "Sunny", "zuri": "Zuri",
}

// SlotsForMC 返回指定 Minecraft Java 版可替换的默认皮肤槽位表。
// 1.19.2 及更早版本仅有 2 个默认皮肤（steve / alex），1.19.3 及之后扩为 18 个。
func SlotsForMC(mc string) ([]SkinSlot, bool, error) {
	maj, minV, patch, ok := parseMCVersion(mc)
	if !ok {
		return nil, false, ErrBadRequest("Minecraft 版本格式认不出来，请填类似 1.20.1 或 1.21.4 的版本号")
	}
	modern := compareTuple(maj, minV, patch, 1, 19, 3) >= 0
	if !modern {
		return []SkinSlot{
			{Index: 0, ID: "steve", Character: "Steve", Model: "wide", ModelLabel: "经典（4px）", Path: "assets/minecraft/textures/entity/steve.png"},
			{Index: 1, ID: "alex", Character: "Alex", Model: "slim", ModelLabel: "纤细（3px）", Path: "assets/minecraft/textures/entity/alex.png"},
		}, false, nil
	}
	slots := make([]SkinSlot, 0, len(modernSkinChars)*2)
	for idx, ch := range modernSkinChars {
		slots = append(slots, SkinSlot{
			Index:      idx,
			ID:         "slim/" + ch,
			Character:  charTitles[ch],
			Model:      "slim",
			ModelLabel: "纤细（3px）",
			Path:       "assets/minecraft/textures/entity/player/slim/" + ch + ".png",
		})
	}
	for idx, ch := range modernSkinChars {
		slots = append(slots, SkinSlot{
			Index:      len(modernSkinChars) + idx,
			ID:         "wide/" + ch,
			Character:  charTitles[ch],
			Model:      "wide",
			ModelLabel: "经典（4px）",
			Path:       "assets/minecraft/textures/entity/player/wide/" + ch + ".png",
		})
	}
	return slots, true, nil
}

// OfflineUUIDBytes 计算离线模式 UUID（RFC 4122 Version 3，MD5("OfflinePlayer:" + player)）。
// 与 internal/inst/players.go 的 offlineUUID 以及 Minecraft 服务端 UUIDUtil.createOfflinePlayerUUID 算法完全一致。
func OfflineUUIDBytes(player string) [16]byte {
	h := md5.Sum([]byte("OfflinePlayer:" + player))
	h[6] = (h[6] & 0x0f) | 0x30
	h[8] = (h[8] & 0x3f) | 0x80
	return h
}

// FormatUUID 将 16 字节 UUID 格式化为标准小写连字符形式。
func FormatUUID(u [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}

// UUIDHashCode 复刻 OpenJDK java.util.UUID.hashCode()：
//
//	long hilo = mostSigBits ^ leastSigBits;
//	return ((int)(hilo >> 32)) ^ (int) hilo;
func UUIDHashCode(u [16]byte) int32 {
	msb := binary.BigEndian.Uint64(u[:8])
	lsb := binary.BigEndian.Uint64(u[8:])
	hilo := msb ^ lsb
	return int32(hilo>>32) ^ int32(hilo)
}

// FloorMod 复刻 OpenJDK Math.floorMod(int, int)（n > 0），避免负数取模得到负索引。
func FloorMod(h int32, n int) int {
	if n <= 0 {
		return 0
	}
	r := int(h) % n
	if r < 0 {
		r += n
	}
	return r
}

// ResolvePlayerSlot 计算离线玩家在目标 Minecraft 版本下命中的默认皮肤槽位。
func ResolvePlayerSlot(player, mc string) (uuidStr string, hash int32, slot SkinSlot, modern bool, err error) {
	slots, modern, err := SlotsForMC(mc)
	if err != nil {
		return "", 0, SkinSlot{}, false, err
	}
	u := OfflineUUIDBytes(player)
	hash = UUIDHashCode(u)
	idx := FloorMod(hash, len(slots))
	return FormatUUID(u), hash, slots[idx], modern, nil
}

// PackFormatForMC 返回对应 Minecraft Java 版的资源包 pack_format 主版本号。
// 对照来源：Minecraft Wiki — Pack format（https://minecraft.wiki/w/Pack_format，2026-11-16 查），涵盖 1.8 至 26.3；
// 比表里更新的版本按最新一档算（客户端会提示「为旧版本制作」，确认后照样能用）。
// 自 1.21.9（69）起 pack.mcmeta 用 min_format / max_format：可以写整数或 [major, minor]；max_format 写整数表示这个
// 大版本下的任何小版本（26.3 是 97.1，写成 [97, 0] 反而把它排除了），所以这里一律写整数。supported_formats 停用。
func PackFormatForMC(mc string) (int, error) {
	maj, minV, patch, ok := parseMCVersion(mc)
	if !ok {
		return 0, ErrBadRequest("Minecraft 版本格式认不出来，请填类似 1.20.1 或 1.21.4 的版本号")
	}
	type rule struct {
		maj, min, patch int
		pf              int
	}
	// 由新到旧匹配下限
	rules := []rule{
		{26, 3, 0, 97},  // 26.3（97.1）及更新
		{26, 2, 0, 88},  // 26.2
		{26, 1, 0, 84},  // 26.1 – 26.1.2
		{1, 21, 11, 75}, // 1.21.11
		{1, 21, 9, 69},  // 1.21.9 – 1.21.10
		{1, 21, 7, 64},  // 1.21.7 – 1.21.8
		{1, 21, 6, 63},  // 1.21.6
		{1, 21, 5, 55},  // 1.21.5
		{1, 21, 4, 46},  // 1.21.4
		{1, 21, 2, 42},  // 1.21.2 – 1.21.3
		{1, 21, 0, 34},  // 1.21 – 1.21.1
		{1, 20, 5, 32},  // 1.20.5 – 1.20.6
		{1, 20, 3, 22},  // 1.20.3 – 1.20.4
		{1, 20, 2, 18},  // 1.20.2
		{1, 20, 0, 15},  // 1.20 – 1.20.1
		{1, 19, 4, 13},  // 1.19.4
		{1, 19, 3, 12},  // 1.19.3
		{1, 19, 0, 9},   // 1.19 – 1.19.2
		{1, 18, 0, 8},   // 1.18 – 1.18.2
		{1, 17, 0, 7},   // 1.17 – 1.17.1
		{1, 16, 2, 6},   // 1.16.2 – 1.16.5
		{1, 15, 0, 5},   // 1.15 – 1.16.1
		{1, 13, 0, 4},   // 1.13 – 1.14.4
		{1, 11, 0, 3},   // 1.11 – 1.12.2
		{1, 9, 0, 2},    // 1.9 – 1.10.2
		{1, 6, 1, 1},    // 1.6.1 – 1.8.9
	}
	for _, r := range rules {
		if compareTuple(maj, minV, patch, r.maj, r.min, r.patch) >= 0 {
			return r.pf, nil
		}
	}
	return 1, nil
}

func parseMCVersion(mc string) (maj, minV, patch int, ok bool) {
	mc = strings.TrimSpace(mc)
	if mc == "" {
		return 1, 21, 4, true
	}
	// 只取前置版本号，忽略例如 "-pre1"、"-rc1" 尾巴
	if i := strings.IndexAny(mc, "-+ "); i >= 0 {
		mc = mc[:i]
	}
	parts := strings.Split(mc, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, 0, false
	}
	vals := [3]int{}
	for idx, p := range parts {
		if p == "" {
			return 0, 0, 0, false
		}
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return 0, 0, 0, false
		}
		vals[idx] = v
	}
	return vals[0], vals[1], vals[2], true
}

func compareTuple(a1, a2, a3, b1, b2, b3 int) int {
	if a1 != b1 {
		if a1 < b1 {
			return -1
		}
		return 1
	}
	if a2 != b2 {
		if a2 < b2 {
			return -1
		}
		return 1
	}
	if a3 != b3 {
		if a3 < b3 {
			return -1
		}
		return 1
	}
	return 0
}

// buildPackMcmeta 生成 pack.mcmeta；minFmt、maxFmt 只在 1.21.9 起的格式里有（其余为 0）。
func buildPackMcmeta(mc, desc string) (pf int, raw []byte, minFmt, maxFmt int, err error) {
	pf, err = PackFormatForMC(mc)
	if err != nil {
		return 0, nil, 0, 0, err
	}
	if strings.TrimSpace(desc) == "" {
		desc = "MineDeck 离线默认皮肤包"
	}
	type packLegacy struct {
		Pack struct {
			PackFormat  int    `json:"pack_format"`
			Description string `json:"description"`
		} `json:"pack"`
	}
	type packModern struct {
		Pack struct {
			PackFormat  int    `json:"pack_format"`
			MinFormat   int    `json:"min_format"`
			MaxFormat   int    `json:"max_format"`
			Description string `json:"description"`
		} `json:"pack"`
	}
	if pf >= 65 {
		var pm packModern
		pm.Pack.PackFormat = pf
		pm.Pack.MinFormat = pf
		pm.Pack.MaxFormat = pf
		pm.Pack.Description = desc
		b, err := json.MarshalIndent(pm, "", "  ")
		return pf, b, pm.Pack.MinFormat, pm.Pack.MaxFormat, err
	}
	var pl packLegacy
	pl.Pack.PackFormat = pf
	pl.Pack.Description = desc
	b, err := json.MarshalIndent(pl, "", "  ")
	return pf, b, 0, 0, err
}

// SkinEntrySpec 一条皮肤配置项（用户表单提交）。
type SkinEntrySpec struct {
	Player string `json:"player"`          // 玩家名（mode="player" 时必填）
	Model  string `json:"model,omitempty"` // wide | slim（sequential 模式用于倾向分配，player 模式下模型由 UUID 决定）
	Slot   int    `json:"slot"`            // sequential 模式可选手动指定槽位，-1 表示自动顺位分配
	Keep   bool   `json:"keep"`            // 当多名玩家撞入同一槽位时，标明选谁保留（true 表示用这一份）
	Skip   bool   `json:"skip"`            // 明确跳过这一条（撞槽时放弃那一个）
}

// SkinPreviewReq 皮肤包槽位预览请求。
type SkinPreviewReq struct {
	MC      string          `json:"mc"`
	Mode    string          `json:"mode"` // player（按玩家名分配） | sequential（按顺序替换）
	Entries []SkinEntrySpec `json:"entries"`
}

// SkinEntryView 预览或生成结果中每条分配详情。
type SkinEntryView struct {
	Index             int      `json:"index"`
	Player            string   `json:"player,omitempty"`
	UUID              string   `json:"uuid,omitempty"`
	HashCode          int32    `json:"hashCode,omitempty"`
	Slot              SkinSlot `json:"slot"`
	Conflict          bool     `json:"conflict,omitempty"`
	Skipped           bool     `json:"skipped,omitempty"`
	Kept              bool     `json:"kept,omitempty"`
	UpgradedFrom64x32 bool     `json:"upgradedFrom64x32,omitempty"`
	Note              string   `json:"note,omitempty"`
}

// SkinConflict 一组撞进同一默认皮肤槽位的玩家提示。
type SkinConflict struct {
	SlotIndex int      `json:"slotIndex"`
	SlotID    string   `json:"slotId"`
	Model     string   `json:"model"`
	Path      string   `json:"path"`
	Players   []string `json:"players"`
	Entries   []int    `json:"entries"`
}

// SkinPreviewResp 槽位预演结果。
type SkinPreviewResp struct {
	MC          string          `json:"mc"`
	Mode        string          `json:"mode"`
	ModernSlots bool            `json:"modernSlots"`
	SlotCount   int             `json:"slotCount"`
	PackFormat  int             `json:"packFormat"`
	MinFormat   int             `json:"minFormat,omitempty"`
	MaxFormat   int             `json:"maxFormat,omitempty"`
	Slots       []SkinSlot      `json:"slots"`
	Items       []SkinEntryView `json:"items"`
	Conflicts   []SkinConflict  `json:"conflicts"`
	Notes       []string        `json:"notes,omitempty"`
}

// PreviewSkinPack 根据目标版本、分配模式和输入条目预演每位玩家命中的默认皮肤槽位及冲突情况。
func PreviewSkinPack(req SkinPreviewReq) (SkinPreviewResp, error) {
	mc := strings.TrimSpace(req.MC)
	if mc == "" {
		mc = "1.21.4"
	}
	mode := normalizeSkinMode(req.Mode)
	slots, modern, err := SlotsForMC(mc)
	if err != nil {
		return SkinPreviewResp{}, err
	}
	pf, _, minFmt, maxFmt, err := buildPackMcmeta(mc, "")
	if err != nil {
		return SkinPreviewResp{}, err
	}
	if len(req.Entries) > MaxSkins {
		return SkinPreviewResp{}, ErrBadRequest(fmt.Sprintf("一次最多放 %d 张皮肤", MaxSkins))
	}

	resp := SkinPreviewResp{
		MC:          mc,
		Mode:        mode,
		ModernSlots: modern,
		SlotCount:   len(slots),
		PackFormat:  pf,
		MinFormat:   minFmt,
		MaxFormat:   maxFmt,
		Slots:       slots,
		Items:       make([]SkinEntryView, 0, len(req.Entries)),
		Conflicts:   []SkinConflict{},
	}
	if !modern {
		resp.Notes = append(resp.Notes, "Minecraft 1.19.2 及更早版本只有 Steve 和 Alex 两个默认皮肤槽位；若人数超过 2 个必然撞槽，建议只配最常用的两位朋友，或切换按顺序替换模式。")
	} else {
		resp.Notes = append(resp.Notes, "在 1.19.3 及以上版本中，离线模式玩家的细手臂（slim）和经典手臂（wide）模型也由玩家名对应的 UUID 唯一决定，资源包只能替换贴图内容、不能把细手臂槽位强行改成宽手臂。")
	}

	if mode == "player" {
		slotUsers := map[int][]int{}
		for i, e := range req.Entries {
			name := strings.TrimSpace(e.Player)
			if name == "" {
				return SkinPreviewResp{}, ErrBadRequest(fmt.Sprintf("第 %d 行还没填玩家名", i+1))
			}
			if len([]rune(name)) > 32 || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
				return SkinPreviewResp{}, ErrBadRequest(fmt.Sprintf("第 %d 行的玩家名「%s」格式不对", i+1, name))
			}
			uuidStr, hash, slot, _, _ := ResolvePlayerSlot(name, mc)
			item := SkinEntryView{
				Index:    i,
				Player:   name,
				UUID:     uuidStr,
				HashCode: hash,
				Slot:     slot,
				Skipped:  e.Skip,
				Kept:     e.Keep,
			}
			if !e.Skip {
				slotUsers[slot.Index] = append(slotUsers[slot.Index], i)
			} else {
				item.Note = "已跳过，不写入材质包"
			}
			resp.Items = append(resp.Items, item)
		}
		for _, sIdx := range slices.Sorted(maps.Keys(slotUsers)) {
			idxs := slotUsers[sIdx]
			if len(idxs) < 2 {
				continue
			}
			players := make([]string, 0, len(idxs))
			for _, idx := range idxs {
				resp.Items[idx].Conflict = true
				players = append(players, resp.Items[idx].Player)
			}
			sl := slots[sIdx]
			resp.Conflicts = append(resp.Conflicts, SkinConflict{
				SlotIndex: sl.Index,
				SlotID:    sl.ID,
				Model:     sl.Model,
				Path:      sl.Path,
				Players:   players,
				Entries:   idxs,
			})
		}
		return resp, nil
	}

	// sequential 模式：先占手动指定的槽位，再给「自动」的挑空着的。跳过的那一条不占槽位，
	// 自动的被跳过时就不给它分槽位（Slot.Index 记 -1）
	used := map[int][]int{}
	targets := make([]int, len(req.Entries))
	for i, e := range req.Entries {
		targets[i] = -1
		if e.Slot >= 0 && e.Slot < len(slots) {
			targets[i] = e.Slot
			if !e.Skip {
				used[e.Slot] = append(used[e.Slot], i)
			}
		}
	}
	for i, e := range req.Entries {
		if targets[i] >= 0 || e.Skip {
			continue
		}
		t := pickFreeSlot(slots, e.Model, used)
		if t < 0 {
			return SkinPreviewResp{}, ErrBadRequest(fmt.Sprintf("Minecraft %s 最多只有 %d 个默认皮肤槽位，放不下第 %d 张皮肤", mc, len(slots), i+1))
		}
		targets[i] = t
		used[t] = append(used[t], i)
	}
	for i, e := range req.Entries {
		item := SkinEntryView{Index: i, Skipped: e.Skip, Kept: e.Keep, Slot: SkinSlot{Index: -1}}
		if targets[i] >= 0 {
			item.Slot = slots[targets[i]]
		}
		if e.Skip {
			item.Note = "已跳过，不写入材质包"
		}
		resp.Items = append(resp.Items, item)
	}
	for _, sIdx := range slices.Sorted(maps.Keys(used)) {
		idxs := used[sIdx]
		if len(idxs) < 2 {
			continue
		}
		slices.Sort(idxs)
		for _, idx := range idxs {
			resp.Items[idx].Conflict = true
		}
		sl := slots[sIdx]
		resp.Conflicts = append(resp.Conflicts, SkinConflict{
			SlotIndex: sl.Index,
			SlotID:    sl.ID,
			Model:     sl.Model,
			Path:      sl.Path,
			Entries:   idxs,
		})
	}
	return resp, nil
}

func pickFreeSlot(slots []SkinSlot, wantModel string, used map[int][]int) int {
	// 优先按手臂模型匹配空闲槽位（先 wide steve / slim alex 这种常见顺序：先尝试完全同模型，再退回任意空闲）
	for _, sl := range slots {
		if len(used[sl.Index]) == 0 && (wantModel == "" || sl.Model == wantModel) {
			return sl.Index
		}
	}
	for _, sl := range slots {
		if len(used[sl.Index]) == 0 {
			return sl.Index
		}
	}
	return -1
}

func normalizeSkinMode(m string) string {
	if strings.EqualFold(strings.TrimSpace(m), "sequential") {
		return "sequential"
	}
	return "player"
}

// SkinBuildReq 打包生成皮肤材质包的请求（UploadedPaths 由操作层验证过仅来自上传缓存目录）。
type SkinBuildReq struct {
	MC            string          `json:"mc"`
	PackName      string          `json:"packName"`
	Description   string          `json:"description"`
	Mode          string          `json:"mode"` // player | sequential
	Entries       []SkinEntrySpec `json:"entries"`
	UploadedPaths map[int]string  `json:"-"` // 第 i 项对应的本地临时文件（安全检查后填入）
	DestDir       string          `json:"destDir"`
}

// SkinBuildResp 打包生成结果。
type SkinBuildResp struct {
	DownloadID   string          `json:"downloadId"`
	FileName     string          `json:"fileName"`
	Size         int64           `json:"size"`
	SHA1         string          `json:"sha1"` // server.properties resource-pack-sha1 用
	PackFormat   int             `json:"packFormat"`
	SlotCount    int             `json:"slotCount"`
	TextureCount int             `json:"textureCount"`
	Items        []SkinEntryView `json:"items"`
	SavedPath    string          `json:"savedPath,omitempty"`
	Notice       string          `json:"notice"`
}

// BuildSkinPack 校验所有皮肤 PNG、检测槽位冲突并打包成可直接放进 .minecraft/resourcepacks/ 的资源包 zip。
func (s *Service) BuildSkinPack(req SkinBuildReq) (SkinBuildResp, error) {
	if len(req.Entries) == 0 {
		return SkinBuildResp{}, ErrBadRequest("至少添加一张皮肤 PNG 图片再生成")
	}
	mc := strings.TrimSpace(req.MC)
	if mc == "" {
		mc = "1.21.4"
	}
	prev, err := PreviewSkinPack(SkinPreviewReq{MC: mc, Mode: req.Mode, Entries: req.Entries})
	if err != nil {
		return SkinBuildResp{}, err
	}
	return s.finishBuildSkinPack(req, prev)
}

func (s *Service) finishBuildSkinPack(req SkinBuildReq, prev SkinPreviewResp) (SkinBuildResp, error) {
	chosenForSlot := map[int]int{}
	for _, c := range prev.Conflicts {
		winners := []int{}
		for _, idx := range c.Entries {
			if req.Entries[idx].Keep {
				winners = append(winners, idx)
			}
		}
		if len(winners) != 1 {
			if len(c.Players) > 0 {
				return SkinBuildResp{}, ErrBadRequest(fmt.Sprintf("玩家 %s 撞进了同一个默认皮肤槽位（%s），请先二选一决定保留谁的皮肤，再点生成",
					strings.Join(c.Players, "、"), c.SlotID))
			}
			return SkinBuildResp{}, ErrBadRequest(fmt.Sprintf("有多张皮肤被指定到了同一个槽位（%s），请调整槽位选择后再生成", c.SlotID))
		}
		chosenForSlot[c.SlotIndex] = winners[0]
	}

	// 先决定最终写入的条目，并对每一张 PNG 做严格解码与尺寸处理（不用上传时的文件名决定任何路径）
	writtenEntries := map[string][]byte{}
	outItems := make([]SkinEntryView, len(prev.Items))
	copy(outItems, prev.Items)
	textureCount := 0

	for i, item := range outItems {
		if item.Skipped {
			continue
		}
		if winner, conflicted := chosenForSlot[item.Slot.Index]; conflicted && winner != i {
			outItems[i].Skipped = true
			outItems[i].Note = "同槽位已选了另一位玩家的皮肤"
			continue
		}
		tmpPath := req.UploadedPaths[i]
		if tmpPath == "" {
			if item.Player != "" {
				return SkinBuildResp{}, ErrBadRequest(fmt.Sprintf("玩家「%s」还没有上传皮肤 PNG 图片", item.Player))
			}
			return SkinBuildResp{}, ErrBadRequest(fmt.Sprintf("第 %d 行还没有上传皮肤 PNG 图片", i+1))
		}
		pngData, upgraded, err := validateAndNormalizeSkinPNG(tmpPath)
		if err != nil {
			prefix := fmt.Sprintf("第 %d 行的皮肤图片有问题：", i+1)
			if item.Player != "" {
				prefix = fmt.Sprintf("玩家「%s」的皮肤图片有问题：", item.Player)
			}
			return SkinBuildResp{}, ErrBadRequest(prefix + err.Error())
		}
		outItems[i].UpgradedFrom64x32 = upgraded
		if winner, ok := chosenForSlot[item.Slot.Index]; ok && winner == i {
			outItems[i].Kept = true
		}
		writtenEntries[item.Slot.Path] = pngData
		textureCount++
	}
	if textureCount == 0 {
		return SkinBuildResp{}, ErrBadRequest("所有皮肤都被跳过了，没有内容可以打包")
	}

	packName := strings.TrimSpace(req.PackName)
	if packName == "" {
		packName = "MineDeck 离线皮肤包"
	}
	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = fmt.Sprintf("%s（适配 Minecraft %s，共 %d 个默认皮肤）", packName, prev.MC, textureCount)
	}
	pf, mcmeta, _, _, err := buildPackMcmeta(prev.MC, desc)
	if err != nil {
		return SkinBuildResp{}, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeZipEntry(zw, "pack.mcmeta", mcmeta); err != nil {
		return SkinBuildResp{}, err
	}
	// 按固定路径写入皮肤贴图（不采用上传原文件名）
	for _, sl := range prev.Slots {
		data, ok := writtenEntries[sl.Path]
		if !ok {
			continue
		}
		if err := writeZipEntry(zw, sl.Path, data); err != nil {
			return SkinBuildResp{}, err
		}
	}
	if err := writeZipEntry(zw, "说明.txt", []byte(buildSkinReadme(packName, prev.MC, pf, outItems))); err != nil {
		return SkinBuildResp{}, err
	}
	if err := zw.Close(); err != nil {
		return SkinBuildResp{}, fmt.Errorf("压缩皮肤材质包失败：%w", err)
	}

	zipBytes := buf.Bytes()
	sum := sha1.Sum(zipBytes)
	sha1Hex := hex.EncodeToString(sum[:])

	stem := safeFileStem(packName, "离线皮肤包")
	fileName := fmt.Sprintf("%s-MC%s-%s.zip", stem, safeFileStem(prev.MC, "pack"), time.Now().Format("20060102-150405"))

	savedPath := ""
	if strings.TrimSpace(req.DestDir) != "" {
		savedPath, err = copyToHostDir(req.DestDir, fileName, zipBytes)
		if err != nil {
			return SkinBuildResp{}, err
		}
	}

	dlEntry, err := s.saveArtifact(fileName, zipBytes, sha1Hex)
	if err != nil {
		return SkinBuildResp{}, err
	}

	return SkinBuildResp{
		DownloadID:   dlEntry.ID,
		FileName:     fileName,
		Size:         dlEntry.Size,
		SHA1:         sha1Hex,
		PackFormat:   pf,
		SlotCount:    prev.SlotCount,
		TextureCount: textureCount,
		Items:        outItems,
		SavedPath:    savedPath,
		Notice:       "资源包需要每位朋友放进各自客户端的 resourcepacks 文件夹并启用才能生效；拥有正版皮肤的在线账号不会被默认皮肤覆盖。",
	}, nil
}

func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	hdr := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	hdr.Modified = time.Now()
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// validateAndNormalizeSkinPNG 严格校验上传的皮肤文件是合法 PNG，尺寸为 64×64 或 64×32；
// 若为老格式 64×32，则按 Minecraft 经典双肢映射规则无损扩展为 64×64（复制上半区并将右腿/右臂镜像填充到左腿/左臂位置）。
func validateAndNormalizeSkinPNG(path string) (outPNG []byte, upgraded bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, fmt.Errorf("读取失败")
	}
	defer f.Close()
	raw, err := readLimited(f, MaxSkinBytes, fmt.Sprintf("超过 %d MB 的皮肤文件不收（正常皮肤只有几 KB）", MaxSkinBytes>>20))
	if err != nil {
		return nil, false, err
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, false, fmt.Errorf("不是有效的 PNG 图片（不能只把别的图片改后缀成 .png）")
	}
	if cfg.Width != 64 || (cfg.Height != 64 && cfg.Height != 32) {
		return nil, false, fmt.Errorf("尺寸是 %dx%d，皮肤只认 64×64 或老格式 64×32", cfg.Width, cfg.Height)
	}
	src, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false, fmt.Errorf("PNG 图片损坏，解不开像素内容")
	}
	dst := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(dst, image.Rect(0, 0, cfg.Width, cfg.Height), src, image.Point{}, draw.Src)
	if cfg.Height == 32 {
		upgraded = true
		// 64×32 老版皮肤升级为 64×64 现代格式：按 Mojang SkinTextureDownloader 的经典 4×12×4 肢体 UV 盒子，
		// 将右腿 (0,16,16×16) -> 左腿 (16,48,16×16)、右臂 (40,16,16×16) -> 左臂 (32,48,16×16) 逐面水平镜像复制，
		// 确保放进 1.8~26.x 资源包的 default wide 槽位不会缺左臂左腿或被纵横拉花。
		copyLimbMirrored(dst, 0, 16, 16, 48)
		copyLimbMirrored(dst, 40, 16, 32, 48)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, false, fmt.Errorf("重新编码 PNG 失败")
	}
	return out.Bytes(), upgraded, nil
}

// copyLimbMirrored 将锚点在 (sx, sy) 的 4×12×4 标准手臂/腿部 UV 展开块按 Mojang 映射规则镜像复制到 (dx, dy)。
func copyLimbMirrored(img *image.NRGBA, sx, sy, dx, dy int) {
	copyFlipX(img, sx+4, sy, dx+4, dy, 4, 4)        // 顶面
	copyFlipX(img, sx+8, sy, dx+8, dy, 4, 4)        // 底面
	copyFlipX(img, sx+0, sy+4, dx+8, dy+4, 4, 12)   // 外侧面 -> 内侧面
	copyFlipX(img, sx+4, sy+4, dx+4, dy+4, 4, 12)   // 正面
	copyFlipX(img, sx+8, sy+4, dx+0, dy+4, 4, 12)   // 内侧面 -> 外侧面
	copyFlipX(img, sx+12, sy+4, dx+12, dy+4, 4, 12) // 背面
}

func copyFlipX(img *image.NRGBA, sx, sy, dx, dy, w, h int) {
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			c := img.NRGBAAt(sx+(w-1-col), sy+row)
			img.SetNRGBA(dx+col, dy+row, c)
		}
	}
}

func buildSkinReadme(packName, mc string, pf int, items []SkinEntryView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\r\n", packName)
	fmt.Fprintf(&b, "适用版本：Minecraft Java Edition %s（pack_format %d）\r\n", mc, pf)
	fmt.Fprintf(&b, "生成时间：%s\r\n\r\n", time.Now().Format("2006-01-02 15:04:05"))
	b.WriteString("使用方法（每位玩家都需要做）：\r\n")
	b.WriteString("1. 把这个 .zip 压缩包发给一起玩的朋友。\r\n")
	b.WriteString("2. 在启动器或游戏客户端里打开「选项 -> 资源包 -> 打开资源包文件夹」（即 .minecraft/resourcepacks/）。\r\n")
	b.WriteString("3. 把本压缩包放进去并启用（不用解压）。\r\n\r\n")
	b.WriteString("说明：\r\n")
	b.WriteString("- 仅对没有正版皮肤的离线玩家生效；正版账号会优先显示各自上传的正版皮肤。\r\n")
	b.WriteString("- 服务端如果同时设置了 resource-pack 也能自动推送（可在 MineDeck 里复制本包的 SHA-1 校验值）。\r\n\r\n")
	b.WriteString("槽位分配清单：\r\n")
	for _, it := range items {
		if it.Skipped {
			continue
		}
		if it.Player != "" {
			fmt.Fprintf(&b, "- 玩家 %s（离线 UUID：%s）-> 槽位 %s -> %s\r\n", it.Player, it.UUID, it.Slot.ID, it.Slot.Path)
		} else {
			fmt.Fprintf(&b, "- 第 %d 张 -> 槽位 %s -> %s\r\n", it.Index+1, it.Slot.ID, it.Slot.Path)
		}
	}
	return b.String()
}
