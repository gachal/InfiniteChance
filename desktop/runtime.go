package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/apikey"
	"github.com/gachal/InfiniteChance/internal/app"
	"github.com/gachal/InfiniteChance/internal/asset"
	"github.com/gachal/InfiniteChance/internal/auth"
	"github.com/gachal/InfiniteChance/internal/canvas"
	"github.com/gachal/InfiniteChance/internal/canvastask"
	"github.com/gachal/InfiniteChance/internal/channel"
	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/health"
	"github.com/gachal/InfiniteChance/internal/objectstore"
	"github.com/gachal/InfiniteChance/internal/pricing"
	"github.com/gachal/InfiniteChance/internal/prompttemplate"
	"github.com/gachal/InfiniteChance/internal/sqlitedb"
	"github.com/gachal/InfiniteChance/internal/usage"
	"github.com/gachal/InfiniteChance/internal/videotask"
	"github.com/gachal/InfiniteChance/internal/wiring"
)

const shutdownGrace = 5 * time.Second

// Desktop owns the in-process services: one SQLite database, one loopback
// listener per service (gateway + canvas, full surfaces — CONTEXT.md 桌面版),
// the canvas worker and the auto-provisioned service key.
type Desktop struct {
	dataDir string
	cfg     Config
	db      *sql.DB
	auths   *auth.SQLiteStore
	tasks   *canvastask.SQLiteStore

	gateway   *app.App
	canvas    *app.App
	workerCtx context.Context
	workerSto context.CancelFunc
}

// NewDesktop loads config, opens the database, provisions the service key
// and assembles both services (not yet listening).
func NewDesktop() (*Desktop, error) {
	// 桌面壳的 gin 请求日志只在开发模式开(INFINITECHANCE_DEV 指向 vite
	// dev server 时保留):发布形态每个请求刷一行日志纯属噪音。
	if os.Getenv("INFINITECHANCE_DEV") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	dataDir, err := DataDir()
	if err != nil {
		return nil, err
	}
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		return nil, err
	}
	db, err := sqlitedb.Open(filepath.Join(dataDir, "app.db"))
	if err != nil {
		return nil, fmt.Errorf("open app database: %w", err)
	}

	ctx := context.Background()

	// 两服务共享同一个库:schema 一次建齐(全部 IF NOT EXISTS,顺序无关)。
	auths := auth.NewSQLiteStore(db)
	channels := channel.NewSQLiteStore(db)
	keys := apikey.NewSQLiteStore(db)
	prices := pricing.NewSQLiteStore(db)
	usageLogs := usage.NewSQLiteStore(db)
	videoTasks := videotask.NewSQLiteStore(db)
	templates := prompttemplate.NewSQLiteStore(db)
	canvases := canvas.NewSQLiteStore(db)
	assets := asset.NewSQLiteStore(db)
	tasks := canvastask.NewSQLiteStore(db, assets)
	for _, s := range []interface {
		EnsureSchema(ctx context.Context) error
	}{
		auths, channels, keys, prices, usageLogs, videoTasks, templates, canvases, assets, tasks,
	} {
		if err := s.EnsureSchema(ctx); err != nil {
			db.Close()
			return nil, fmt.Errorf("ensure schema: %w", err)
		}
	}

	if err := ensureServiceKey(ctx, keys, &cfg); err != nil {
		db.Close()
		return nil, err
	}
	// 首启时把生成的 JWT secret / service key 落盘,之后的启动复用。
	if err := cfg.Save(dataDir); err != nil {
		db.Close()
		return nil, err
	}

	// 产物对象存储:建不出来只影响素材转存,不拦启动(与服务器形态一致)。
	assetsDir := filepath.Join(dataDir, "assets")
	storage, err := objectstore.NewFileSystem(assetsDir)
	if err != nil {
		log.Printf("WARNING: 素材对象存储不可用(%v),生成产物将无法转存", err)
	}

	gatewayURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.GatewayPort)
	canvasURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.CanvasPort)
	canvasTarget, err := url.Parse(canvasURL)
	if err != nil {
		db.Close()
		return nil, err
	}

	base := config.Config{
		JWTSecret:             cfg.JWTSecret,
		GatewayBaseURL:        gatewayURL,
		CanvasServiceKey:      cfg.CanvasServiceKey,
		CanvasTaskConcurrency: cfg.CanvasTaskConcurrency,
		AssetStorageDir:       assetsDir,
	}
	gcfg := base
	gcfg.Name = "gateway"
	gcfg.Port = strconv.Itoa(cfg.GatewayPort)
	ccfg := base
	ccfg.Name = "canvas"
	ccfg.Port = strconv.Itoa(cfg.CanvasPort)

	pingers := map[string]health.Pinger{"sqlite": health.SQLite{DB: db}}

	gatewayApp, err := app.Assemble("gateway", "8080", func(r *gin.Engine, d app.Deps) {
		wiring.GatewayRoutes(r, d.Config, wiring.GatewayStores{
			Auth:            auths,
			Channels:        channels,
			Keys:            keys,
			Prices:          prices,
			UsageLogs:       usageLogs,
			VideoTasks:      videoTasks,
			PromptTemplates: templates,
		})
	}, app.WithConfig(gcfg), app.WithDB(db), app.WithPingers(pingers),
		app.WithHTTPHandler(func(h http.Handler) http.Handler {
			// 管理台源:/api → 网关自身,/canvas-api → 画布服务(与
			// admin-web 的 vite/nginx 代理同形)。
			return newOriginMux(h, adminSPA(), map[string]http.Handler{
				"/canvas-api": proxyTo(canvasTarget),
			})
		}))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("assemble gateway: %w", err)
	}

	canvasApp, err := app.Assemble("canvas", "8081", func(r *gin.Engine, d app.Deps) {
		wiring.CanvasRoutes(r, d.Config, wiring.CanvasStores{
			Auth:      auths,
			Canvases:  canvases,
			Assets:    assets,
			Prices:    prices,
			Tasks:     tasks,
			Templates: templates,
		}, canvastask.NewClient(gatewayURL, cfg.CanvasServiceKey), storage)
	}, app.WithConfig(ccfg), app.WithDB(db), app.WithPingers(pingers),
		app.WithHTTPHandler(func(h http.Handler) http.Handler {
			// 画布源:/api → 画布自身(与 canvas-web 的代理同形)。
			return newOriginMux(h, canvasSPA(), nil)
		}))
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("assemble canvas: %w", err)
	}

	return &Desktop{
		dataDir: dataDir,
		cfg:     cfg,
		db:      db,
		auths:   auths,
		tasks:   tasks,
		gateway: gatewayApp,
		canvas:  canvasApp,
	}, nil
}

// Start binds both loopback listeners. A port conflict fails the whole boot
// synchronously(共识 Q6):调用方在窗口里明确报错,而不是静默换端口。
func (dt *Desktop) Start() error {
	if err := dt.canvas.Start(); err != nil {
		return fmt.Errorf("画布服务启动失败(端口 %s 被占用?):%w", dt.canvas.Config.Port, err)
	}
	if err := dt.gateway.Start(); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		_ = dt.canvas.Stop(ctx)
		return fmt.Errorf("网关服务启动失败(端口 %s 被占用?):%w", dt.gateway.Config.Port, err)
	}

	// 画布 worker:孤儿回队 + 后台驱动;应用开启期间任务持续运行,
	// 下次启动由 RequeueRunning 恢复(与服务器形态同一套恢复语义)。
	dt.workerCtx, dt.workerSto = context.WithCancel(context.Background())
	if n, err := dt.tasks.RequeueRunning(context.Background()); err != nil {
		log.Printf("canvastask: requeue running: %v", err)
	} else if n > 0 {
		log.Printf("canvastask: requeued %d orphaned task(s) from the previous run", n)
	}
	worker := canvastask.NewWorker(dt.tasks, canvastask.NewClient(
		fmt.Sprintf("http://127.0.0.1:%d", dt.cfg.GatewayPort), dt.cfg.CanvasServiceKey,
	), canvastask.WithConcurrency(dt.cfg.CanvasTaskConcurrency))
	go worker.Run(dt.workerCtx)
	return nil
}

// Shutdown gracefully stops worker, listeners and the database.
func (dt *Desktop) Shutdown() {
	if dt.workerSto != nil {
		dt.workerSto()
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	_ = dt.canvas.Stop(ctx)
	_ = dt.gateway.Stop(ctx)
	_ = dt.db.Close()
}

// Initialized reports whether the admin account exists yet — the shell
// auto-opens the admin console alongside the canvas on first run.
func (dt *Desktop) Initialized() bool {
	initialized, err := dt.auths.Initialized(context.Background())
	return err == nil && initialized
}

func (dt *Desktop) CanvasURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", dt.cfg.CanvasPort)
}

func (dt *Desktop) GatewayURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", dt.cfg.GatewayPort)
}
