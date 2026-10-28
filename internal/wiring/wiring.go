// Package wiring mounts both services' route trees on a gin engine.
//
// The store bundles are dialect-agnostic interfaces, so the same wiring
// serves the MySQL server binaries and the desktop shell's SQLite stores
// (ADR 0001). Schema creation is NOT wiring's job — every store's
// EnsureSchema must have run before its bundle is passed in.
package wiring

import (
	"log"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/character"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/musictask"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/promptgen"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/relay"
	"github.com/gachal/InfiniteChance/internal/settings"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
	"github.com/gachal/InfiniteChance/internal/voice"
)

// GatewayStores carries the wired gateway stores.
type GatewayStores struct {
	Auth       auth.Store
	Channels   channel.Store
	Keys       apikey.Store
	Prices     pricing.Store
	UsageLogs  usage.Store
	VideoTasks videotask.Store
	// MusicTasks 承载配乐异步任务(60 号票):/v1/audio/music 两面的
	// 持久层,与 VideoTasks 平行互不混用。
	MusicTasks      musictask.Store
	PromptTemplates prompttemplate.Store
	// Settings backs the admin storage-config CRUD (19 号票);canvas/server
	// reads the same table.
	Settings settings.Store
	// Assets 与 Storage 支撑直连生图产物转存(23 号票 relay_persist):
	// 落 assets 表行与 settings 驱动的云桶。任一为 nil = 不装配,直连
	// 产物照旧透传厂商临时 URL。
	Assets  asset.Store
	Storage *objectstore.Dynamic
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
	// 素材哈希回填与去重(48 号票):素材行与对象存储都就位才挂;store
	// 须实现 BackfillStore(两方言 store 均实现)。
	if s.Assets != nil && s.Storage != nil {
		if bfs, ok := s.Assets.(asset.BackfillStore); ok {
			asset.RegisterBackfillRoutes(admin.Group("/assets/hash-backfill"),
				&asset.Backfill{Store: bfs, Storage: s.Storage})
		} else {
			log.Printf("wiring: asset store does not support hash backfill, /admin/assets/hash-backfill not mounted")
		}
	}

	v1 := r.Group("/v1", apikey.RequireKey(s.Keys))
	// relay_persist(23 号票):cloud store 与 asset 行都就位才装配;
	// 未装配时 rewriteBody 根本不会被调用。
	var persist *relay.ImagePersist
	if s.Storage != nil && s.Assets != nil {
		persist = &relay.ImagePersist{Cloud: s.Storage, Assets: s.Assets}
	}
	relay.RegisterRoutes(v1, &relay.Handlers{
		Channels:   s.Channels,
		Keys:       s.Keys,
		Prices:     s.Prices,
		Usage:      s.UsageLogs,
		Tasks:      s.VideoTasks,
		MusicTasks: s.MusicTasks,
		CacheTTL:   relay.DefaultCacheTTL,
		Persist:    persist,
	})
}

// CanvasStores carries the wired canvas stores. Templates is read-only on
// the canvas side — the table is administered through the gateway. Settings
// is read-only the same way (19 号票): the storage row drives the object
// store's dynamic routing and the assets' public-address resolution.
// IPs 支撑画布创建/移动的 IP 校验、/ips CRUD 与素材上传的存在性检查
// (50 号票);nil 时校验缺口以运行时 500 暴露,生产装配恒非 nil。
// Characters 承载 IP 级角色库(51 号票):/api/ips/:id/characters 目录与
// /api/characters 系 CRUD,与 IPs 同用 IP 存在性裁决。
// Voices 承载音色台账(57 号票):/api/voices 目录面(全局作用域,无
// ip_id),nil 时目录 500 暴露装配缺口(生产装配恒非 nil)。
type CanvasStores struct {
	Auth       auth.Store
	Canvases   canvas.Store
	IPs        canvas.IPStore
	Characters character.Store
	Voices     voice.Store
	Assets     asset.Store
	Prices     pricing.Store
	Tasks      canvastask.Store
	Templates  prompttemplate.Store
	Settings   settings.Store
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
	canvas.RegisterRoutes(group, &canvas.Handlers{Store: s.Canvases, IPs: s.IPs})
	canvastask.RegisterRoutes(group, &canvastask.Handlers{
		Tasks:         s.Tasks,
		Canvases:      s.Canvases,
		Models:        s.Prices,
		Assets:        s.Assets,
		Gateway:       gateway,
		Voices:        s.Voices,
		PublicBaseURL: publicBase,
	})
	// IP 企划面(50 号票):CRUD 挂 JWT 会话,与画布同一令牌体系;
	// admin-web 不设 IP 管理页,增删改全在创作侧。
	ipsGroup := r.Group("/ips", auth.RequireAuth(issuer))
	canvas.RegisterIPRoutes(ipsGroup, &canvas.IPHandlers{Store: s.IPs})
	// 角色库(51 号票):IP 级角色、造型变体与设定图锚,同一 JWT 会话面。
	// 目录挂 /ips/:id/characters(IP 存在性由 handler 经 IPs 裁决),
	// 角色系 CRUD 与变体/图面挂 /characters 系;绑定音色的实时校准(57
	// 号票评审 F1)与音色台账删除面共用同一个网关 client 的切片断言 ——
	// gateway 为 nil 时绑定以 gateway_unconfigured 拒绝,其余字段不受影响。
	var voiceGateway voice.UpstreamRemover
	var voiceQuerier voice.UpstreamQuerier
	if gateway != nil {
		if remover, ok := gateway.(voice.UpstreamRemover); ok {
			voiceGateway = remover
		}
		if querier, ok := gateway.(voice.UpstreamQuerier); ok {
			voiceQuerier = querier
		}
	}
	characterHandlers := &character.Handlers{Store: s.Characters, IPs: s.IPs, Voices: s.Voices, Gateway: voiceQuerier}
	character.RegisterIPRoutes(r.Group("/ips/:id/characters", auth.RequireAuth(issuer)), characterHandlers)
	character.RegisterRoutes(r.Group("/characters", auth.RequireAuth(issuer)), characterHandlers)
	character.RegisterVariantRoutes(r.Group("/character-variants", auth.RequireAuth(issuer)), characterHandlers)
	character.RegisterImageRoutes(r.Group("/character-images", auth.RequireAuth(issuer)), characterHandlers)
	// 音色台账(57 号票):/voices 目录面(全量/改名/删除)。删除先经网关
	// delete_voice —— gateway(client)为 nil 时 handler 以
	// gateway_unconfigured 拒绝,列表与改名不受影响。
	voice.RegisterRoutes(r.Group("/voices", auth.RequireAuth(issuer)),
		&voice.Handlers{Store: s.Voices, Gateway: voiceGateway})
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
		Characters:    s.Characters,
		Models:        s.Prices,
		Gateway:       chatGateway,
		PublicBaseURL: publicBase,
	})

	// 目录面:图像/视频/音频模型与提示词模板、提示词聊天模型,全挂 JWT
	// 会话。/audio-models 以服务级 key 代理网关 /v1/audio/models(57 号票
	// —— audio 能力过滤维度在渠道表,归网关管)。
	canvastask.RegisterModelRoutes(r.Group("/image-models", auth.RequireAuth(issuer)),
		&canvastask.ModelHandlers{Prices: s.Prices})
	canvastask.RegisterVideoModelRoutes(r.Group("/video-models", auth.RequireAuth(issuer)),
		&canvastask.ModelHandlers{Prices: s.Prices})
	canvastask.RegisterAudioModelRoutes(r.Group("/audio-models", auth.RequireAuth(issuer)),
		&canvastask.Handlers{Gateway: gateway, Voices: s.Voices})
	// /music-models 以服务级 key 代理网关 /v1/audio/music-models(60 号票
	// —— music 能力过滤维度在渠道表,归网关管)。
	canvastask.RegisterMusicModelRoutes(r.Group("/music-models", auth.RequireAuth(issuer)),
		&canvastask.Handlers{Gateway: gateway, Voices: s.Voices})
	// /sfx-models 以服务级 key 代理网关 /v1/audio/sfx-models(61 号票
	// —— sfx 能力过滤维度在渠道表,归网关管)。
	canvastask.RegisterSfxModelRoutes(r.Group("/sfx-models", auth.RequireAuth(issuer)),
		&canvastask.Handlers{Gateway: gateway, Voices: s.Voices})
	promptgen.RegisterCatalogRoutes(r.Group("/prompt-templates", auth.RequireAuth(issuer)),
		&promptgen.CatalogHandlers{Templates: s.Templates})
	promptgen.RegisterModelRoutes(r.Group("/prompt-models", auth.RequireAuth(issuer)),
		&promptgen.ModelHandlers{Prices: s.Prices})

	// 素材内容寻址例外 —— 节点用 <img>/<video> 预览,带不了
	// Authorization 头(见 asset 包);素材库的列表/删除挂 JWT 会话,
	// 画布素材面板与管理端素材页共用。thumb 端点(49 号票)同组同理由,
	// PublicBaseURL 与画布任务/提示词生成共用同一 settings 提供者。
	assetHandlers := &asset.Handlers{Store: s.Assets, Storage: storage, PublicBaseURL: publicBase, IPs: s.IPs}
	asset.RegisterContentRoutes(r.Group("/assets"), assetHandlers)
	asset.RegisterLibraryRoutes(r.Group("/assets", auth.RequireAuth(issuer)), assetHandlers)
}
