package catalog

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"minedeck/internal/app"
	"minedeck/internal/modcart"
	"minedeck/internal/ops"
	"minedeck/internal/toolbox"
)

// SkinUploadDir 网页层接收上传皮肤 PNG 时的暂存目录（<cache>/toolbox/skin-upload）；
// 操作层只允许读取落在这个目录里的普通文件。
func SkinUploadDir() string { return filepath.Join(app.CacheDir, "toolbox", "skin-upload") }

func toolboxErr(err error) error {
	var e *toolbox.Error
	if errors.As(err, &e) {
		return ops.Wrap(e.Code, e.Err)
	}
	// cart.json 在却没读成：503，界面认这个状态码并显示提示（同 cartErr）
	if le := (*modcart.LoadError)(nil); errors.As(err, &le) {
		return ops.Wrap(http.StatusServiceUnavailable, err)
	}
	return err
}

func (c *cat) toolboxOps() {
	c.reg.Register(&ops.Op{
		Name: "toolbox.skinpack.preview", Title: "预览离线皮肤槽位分配", Level: ops.HumanOnly, NoLog: true,
		Desc: "按 Minecraft 版本和玩家名或顺序分配模式，算出离线 UUID、默认皮肤槽位、pack_format 和撞槽提醒。",
		Params: ops.Object(map[string]*ops.Schema{
			"mc":   ops.String("目标 Minecraft 版本，如 1.21.4").Len(0, 40),
			"mode": ops.String("分配模式：player（按玩家名算 UUID）或 sequential（按顺序替换）").OneOf("player", "sequential"),
			"entries": ops.Array("皮肤配置行", ops.Object(map[string]*ops.Schema{
				"player": ops.String("玩家名"),
				"model":  ops.String("手臂模型：wide 或 slim"),
				"slot":   ops.Integer("指定槽位索引"),
				"keep":   ops.Boolean("撞槽时是否保留这一条"),
				"skip":   ops.Boolean("撞槽时是否跳过这一条"),
			})).MaxLen(toolbox.MaxSkins),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			req, err := bind[toolbox.SkinPreviewReq](a)
			if err != nil {
				return nil, err
			}
			res, err := toolbox.PreviewSkinPack(req)
			if err != nil {
				return nil, toolboxErr(err)
			}
			return res, nil
		},
	})

	type skinBuildArgs struct {
		MC          string                  `json:"mc"`
		PackName    string                  `json:"packName"`
		Description string                  `json:"description"`
		Mode        string                  `json:"mode"`
		Entries     []toolbox.SkinEntrySpec `json:"entries"`
		Files       map[string]string       `json:"files"` // 行下标 string -> 上传暂存目录内临时文件路径
		DestDir     string                  `json:"destDir"`
	}
	c.reg.Register(&ops.Op{
		Name: "toolbox.skinpack.build", Title: "生成离线皮肤资源包", Level: ops.HumanOnly,
		Desc: "校验上传的皮肤 PNG（必须合法解码且为 64×64 或 64×32）并打包成客户端可用资源包 .zip，计算 SHA-1；给出 destDir 时须为本机请求。",
		Params: ops.Object(map[string]*ops.Schema{
			"mc":          ops.String("目标 Minecraft 版本").Len(0, 40),
			"packName":    ops.String("材质包名称").Len(0, 100),
			"description": ops.String("pack.mcmeta 描述").Len(0, 200),
			"mode":        ops.String("分配模式").OneOf("player", "sequential"),
			"destDir":     ops.String("可选：同时保存到本机哪个文件夹（仅本机可用）").Len(0, 400),
		}),
		Run: func(_ context.Context, caller ops.Caller, a ops.Args) (any, error) {
			p, err := bind[skinBuildArgs](a)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(p.DestDir) != "" && (!caller.Local || caller.Source.AI()) {
				return nil, ops.Errorf(403, "保存到本机指定文件夹仅支持在本机操作（远程管理时请直接点击下载）")
			}
			allowedDir := filepath.Clean(c.Toolbox.SkinUploadDir())
			uploads := make(map[int]string, len(p.Files))
			for k, rawPath := range p.Files {
				idx, err := strconv.Atoi(k)
				if err != nil || idx < 0 || idx >= toolbox.MaxSkins {
					return nil, ops.Errorf(400, "皮肤文件索引不对")
				}
				clean := filepath.Clean(rawPath)
				if filepath.Dir(clean) != allowedDir {
					return nil, ops.Errorf(400, "上传皮肤路径不合法")
				}
				lst, err := os.Lstat(clean)
				if err != nil || !lst.Mode().IsRegular() {
					return nil, ops.Errorf(400, "找不到刚才上传的皮肤图片，请重新选择")
				}
				uploads[idx] = clean
			}
			res, err := c.Toolbox.BuildSkinPack(toolbox.SkinBuildReq{
				MC:            p.MC,
				PackName:      p.PackName,
				Description:   p.Description,
				Mode:          p.Mode,
				Entries:       p.Entries,
				UploadedPaths: uploads,
				DestDir:       p.DestDir,
			})
			if err != nil {
				return nil, toolboxErr(err)
			}
			return res, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "toolbox.mrpack.draft.get", Title: "读取工具箱整合包草稿设置", Level: ops.HumanOnly, NoLog: true,
		Desc:   "返回工具箱里暂存的整合包名称、版本号、加载器和 MC 版本（重启不丢失）。",
		Params: ops.Object(map[string]*ops.Schema{}),
		Run: func(_ context.Context, _ ops.Caller, _ ops.Args) (any, error) {
			cfg, err := c.Toolbox.ReadMrpackDraft()
			if err != nil {
				return nil, ops.Wrap(500, err)
			}
			return cfg, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "toolbox.mrpack.draft.save", Title: "保存工具箱整合包草稿设置", Level: ops.HumanOnly, NoLog: true,
		Desc: "持久化工具箱整合包草稿元数据，便于下次打开程序继续挑选模组。只改传了的字段（name、versionId、summary、core、mc、" +
			"loaderVersion、includeConfig 各自给了才改，没传的沿用存好的），返回合起来的整份草稿；存好的草稿读不出来时报错、不保存。",
		Params: ops.Object(map[string]*ops.Schema{
			"name":          ops.String("整合包名称").Len(0, 100),
			"versionId":     ops.String("版本号").Len(0, 50),
			"summary":       ops.String("简介").Len(0, 300),
			"core":          ops.String("加载器核心：fabric / forge / neoforge").OneOf("", "fabric", "forge", "neoforge"),
			"mc":            ops.String("Minecraft 版本").Len(0, 40),
			"loaderVersion": ops.String("加载器版本").Len(0, 80),
			"includeConfig": ops.Boolean("从实例导出时是否带上 config"),
		}),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			// 只取传了的字段（两个窗口同时开着时，没刷新的那一边不把另一边刚改的字段改回去，2026-11-17）
			p, err := bind[toolbox.DraftPatch](a)
			if err != nil {
				return nil, err
			}
			// 太长报 400；存好的草稿读不出来是 *toolbox.Error 带的 500（同 draft.get）；写盘失败照旧
			out, err := c.Toolbox.PatchMrpackDraft(p)
			if err != nil {
				return nil, toolboxErr(err)
			}
			return out, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "toolbox.mrpack.preview", Title: "预览 Modrinth 整合包导出清单", Level: ops.HumanOnly, NoLog: true,
		Desc: "扫描已有模组服务器或工具箱草稿清单，匹配 Modrinth 下载链接和 SHA-512，区分在线下载项与 overrides 覆盖文件。",
		Params: ops.Object(map[string]*ops.Schema{
			"source":        ops.String("来源：instance 或 draft").OneOf("", "instance", "draft"),
			"inst":          ops.String("已有服务器名称").Len(0, 100),
			"name":          ops.String("整合包名称").Len(0, 100),
			"versionId":     ops.String("版本号").Len(0, 50),
			"summary":       ops.String("简介").Len(0, 300),
			"core":          ops.String("加载器：fabric / forge / neoforge").Len(0, 20),
			"mc":            ops.String("Minecraft 版本").Len(0, 40),
			"loaderVersion": ops.String("加载器版本").Len(0, 80),
			"includeConfig": ops.Boolean("是否打包 config 文件夹"),
			"withDeps":      ops.Boolean("草稿模式是否自动带出前置依赖"),
		}),
		Run: func(ctx context.Context, _ ops.Caller, a ops.Args) (any, error) {
			req, err := bind[toolbox.MrpackReq](a)
			if err != nil {
				return nil, err
			}
			cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			res, err := c.Toolbox.PreviewMrpack(cctx, req)
			if err != nil {
				return nil, toolboxErr(err)
			}
			return res, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "toolbox.mrpack.build", Title: "生成 Modrinth 整合包（.mrpack）", Level: ops.HumanOnly,
		Desc: "按预览结果生成标准 .mrpack 文件；destDir 仅限本机请求指定。",
		Params: ops.Object(map[string]*ops.Schema{
			"source":        ops.String("来源：instance 或 draft").OneOf("", "instance", "draft"),
			"inst":          ops.String("已有服务器名称").Len(0, 100),
			"name":          ops.String("整合包名称").Len(0, 100),
			"versionId":     ops.String("版本号").Len(0, 50),
			"summary":       ops.String("简介").Len(0, 300),
			"core":          ops.String("加载器").Len(0, 20),
			"mc":            ops.String("Minecraft 版本").Len(0, 40),
			"loaderVersion": ops.String("加载器版本").Len(0, 80),
			"includeConfig": ops.Boolean("是否打包 config 文件夹"),
			"withDeps":      ops.Boolean("草稿模式是否自动带出前置依赖"),
			"destDir":       ops.String("可选：保存到本机哪个文件夹（仅本机可用）").Len(0, 400),
		}),
		Run: func(ctx context.Context, caller ops.Caller, a ops.Args) (any, error) {
			req, err := bind[toolbox.MrpackReq](a)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(req.DestDir) != "" && (!caller.Local || caller.Source.AI()) {
				return nil, ops.Errorf(403, "保存到本机指定文件夹仅支持在本机操作（远程管理时请直接点击下载）")
			}
			cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			res, err := c.Toolbox.BuildMrpack(cctx, req)
			if err != nil {
				return nil, toolboxErr(err)
			}
			return res, nil
		},
	})

	c.reg.Register(&ops.Op{
		Name: "toolbox.download", Title: "取回工具箱生成的下载记录", Level: ops.HumanOnly, NoLog: true,
		Desc:   "按下载编号取出工具箱生成的 .zip 或 .mrpack 缓存文件路径。",
		Params: ops.Object(map[string]*ops.Schema{"id": ops.String("生成结果返回的 downloadId").Len(24, 24)}, "id"),
		Run: func(_ context.Context, _ ops.Caller, a ops.Args) (any, error) {
			p, err := bind[struct {
				ID string `json:"id"`
			}](a)
			if err != nil {
				return nil, err
			}
			entry, err := c.Toolbox.GetDownload(p.ID)
			if err != nil {
				return nil, toolboxErr(err)
			}
			return entry, nil
		},
	})
}
