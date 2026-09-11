// Package wiring mounts both services' route trees on a gin engine.
//
// The store bundles are dialect-agnostic interfaces, so the same wiring
// serves the MySQL server binaries and the desktop shell's SQLite stores
// (ADR 0001). Schema creation is NOT wiring's job — every store's
// EnsureSchema must have run before its bundle is passed in.
package wiring

import (
	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/relay"
	"github.com/gachal/InfiniteChance/internal/settings"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
)

// GatewayStores carries the wired gateway stores.
type GatewayStores struct {
	Auth            auth.Store
	Channels        channel.Store
	Keys            apikey.Store
	Prices          pricing.Store
	UsageLogs       usage.Store
	VideoTasks      videotask.Store
	PromptTemplates prompttemplate.Store
	// Settings backs the admin storage-config CRUD (19 号票);canvas/server
	// reads the same table.
	Settings settings.Store
}

// GatewayRoutes mounts the gateway surface: /auth (public init/login),
// /admin behind the JWT session, /v1 behind the API key.
func GatewayRoutes(r *gin.Engine, cfg config.Config, s GatewayStores) {
	issuer := auth.NewIssuerFromConfig(cfg)
	auth.RegisterRoutes(r, &auth.Handlers{Store: s.Auth, Issuer: issuer})

	// 管理面:统一走 JWT 会话;中转面(/v1)统一走 apikey.RequireKey,
	// 两者互不混用。
	admin := r.Group("/admin", auth.RequireAuth(issuer))
	channel.RegisterAdminRoutes(admin, &channel.Handlers{
		Store:  s.Channels,
		Tester: &channel.Tester{},
	})
	apikey.RegisterAdminRoutes(admin, &apikey.Handlers{Store: s.Keys})
	pricing.RegisterAdminRoutes(admin, &pricing.Handlers{Store: s.Prices})
	// 提示词模板:管理端维护,画布侧经共享库即时读取(11 号票)。
	prompttemplate.RegisterAdminRoutes(admin, &prompttemplate.Handlers{Store: s.PromptTemplates})
	// 动态配置:存储驱动的管理面(19 号票),画布侧同库只读。
	settings.RegisterAdminRoutes(admin, &settings.Handlers{Store: s.Settings})
	// 用量审计:请求级日志列表与按天/模型/渠道汇总(15 号票)。
	usage.RegisterAdminRoutes(admin, &usage.Handlers{Store: s.UsageLogs})

	v1 := r.Group("/v1", apikey.RequireKey(s.Keys))
	relay.RegisterRoutes(v1, &relay.Handlers{
		Channels: s.Channels,
		Keys:     s.Keys,
		Prices:   s.Prices,
		Usage:    s.UsageLogs,
		Tasks:    s.VideoTasks,
		CacheTTL: relay.DefaultCacheTTL,
	})
}

// CanvasStores carries the wired canvas stores. Templates is read-only on
// the canvas side — the table is administered through the gateway. Settings
// is read-only the same way (19 号票): the storage row drives the object
// store's dynamic routing and the assets' public-address resolution.
type CanvasStores struct {
	Auth      auth.Store
	Canvases  canvas.Store
	Assets    asset.Store
	Prices    pricing.Store
	Tasks     canvastask.Store
	Templates prompttemplate.Store
	Settings  settings.Store
}

// CanvasRoutes mounts the canvas surface: /auth (same account table),
// JWT-gated /canvases + catalogs, and the deliberately unauthenticated
// /assets/:id/content preview route. gateway is the pre-built service-key
// client (nil = unconfigured: submits refuse with gateway_unconfigured);
// storage backs asset archiving (nil = archiving failures recorded on
// tasks, service still boots) — 19 号票起装配侧传 settings 驱动的
// Dynamic,local 驱动时行为与此前逐字节一致。
func CanvasRoutes(r *gin.Engine, cfg config.Config, s CanvasStores, gateway canvastask.Gateway, storage objectstore.Store) {
	issuer := auth.NewIssuerFromConfig(cfg)
	auth.RegisterRoutes(r, &auth.Handlers{Store: s.Auth, Issuer: issuer})

	// 公网地址提供者(18 号票接缝 → 19 号票接线):解析顺序里的自有存储
	// 公网地址按请求读 settings,未配置即 nil = 现状行为。
	publicBase := settings.PublicBaseURL(s.Settings)

	// 画布面:一律先过 JWT 会话,与管理面同一套令牌体系。
	group := r.Group("/canvases", auth.RequireAuth(issuer))
	canvas.RegisterRoutes(group, &canvas.Handlers{Store: s.Canvases})
	canvastask.RegisterRoutes(group, &canvastask.Handlers{
		Tasks:         s.Tasks,
		Canvases:      s.Canvases,
		Models:        s.Prices,
		Assets:        s.Assets,
		Gateway:       gateway,
		PublicBaseURL: publicBase,
	})
	// 提示词生成与画布任务共用同一服务 key:client 为 nil 时动作
	// 直接以 gateway_unconfigured 拒绝。
	var chatGateway promptgen.Gateway
	if gateway != nil {
		chatGateway = promptgen.NewClient(cfg.GatewayBaseURL, cfg.CanvasServiceKey)
	}
	promptgen.RegisterRoutes(group, &promptgen.Handlers{
		Templates:     s.Templates,
		Canvases:      s.Canvases,
		Assets:        s.Assets,
		Models:        s.Prices,
		Gateway:       chatGateway,
		PublicBaseURL: publicBase,
	})

	// 目录面:图像/视频模型与提示词模板、提示词聊天模型,全挂 JWT 会话。
	canvastask.RegisterModelRoutes(r.Group("/image-models", auth.RequireAuth(issuer)),
		&canvastask.ModelHandlers{Prices: s.Prices})
	canvastask.RegisterVideoModelRoutes(r.Group("/video-models", auth.RequireAuth(issuer)),
		&canvastask.ModelHandlers{Prices: s.Prices})
	promptgen.RegisterCatalogRoutes(r.Group("/prompt-templates", auth.RequireAuth(issuer)),
		&promptgen.CatalogHandlers{Templates: s.Templates})
	promptgen.RegisterModelRoutes(r.Group("/prompt-models", auth.RequireAuth(issuer)),
		&promptgen.ModelHandlers{Prices: s.Prices})

	// 素材内容寻址例外 —— 节点用 <img>/<video> 预览,带不了
	// Authorization 头(见 asset 包);素材库的列表/删除挂 JWT 会话,
	// 画布素材面板与管理端素材页共用。
	assetHandlers := &asset.Handlers{Store: s.Assets, Storage: storage}
	asset.RegisterContentRoutes(r.Group("/assets"), assetHandlers)
	asset.RegisterLibraryRoutes(r.Group("/assets", auth.RequireAuth(issuer)), assetHandlers)
}
