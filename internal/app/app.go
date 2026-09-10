// Package app wires one service binary together: env config, MySQL/Redis
// dependency pingers, the shared HTTP server — then blocks serving.
//
// Assemble/Start/Stop expose the same wiring non-blocking so the desktop
// shell can host the services in-process with its own config source, store
// dialect and dependency set (see desktop/); Run stays the blocking path
// used by both server binaries.
package app

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gachal/InfiniteChance/internal/config"
	"github.com/gachal/InfiniteChance/internal/health"
	"github.com/gachal/InfiniteChance/internal/server"
)

// Deps carries the wired dependencies to a binary's route registration.
type Deps struct {
	Config config.Config
	DB     *sql.DB
}

// Option overrides a default wiring decision for Assemble.
type Option func(*options)

type options struct {
	cfg     *config.Config
	db      *sql.DB
	pingers map[string]health.Pinger
	// wrap decorates the assembled engine as the root HTTP handler (the
	// desktop wraps it with SPA static hosting + API aliasing).
	wrap func(http.Handler) http.Handler
}

// WithConfig supplies a fully built config instead of loading env vars —
// the desktop shell builds one from its local config file.
func WithConfig(cfg config.Config) Option {
	return func(o *options) { o.cfg = &cfg }
}

// WithDB supplies an open database (any driver) instead of opening MySQL
// from the config DSN.
func WithDB(db *sql.DB) Option {
	return func(o *options) { o.db = db }
}

// WithPingers supplies the /healthz dependency set instead of the default
// MySQL+Redis pair — the desktop assembly pings SQLite only.
func WithPingers(pingers map[string]health.Pinger) Option {
	return func(o *options) { o.pingers = pingers }
}

// WithHTTPHandler decorates the assembled engine as the served root
// handler — the desktop wraps it with SPA static hosting and API aliasing.
func WithHTTPHandler(wrap func(http.Handler) http.Handler) Option {
	return func(o *options) { o.wrap = wrap }
}

// App is an assembled service: routes registered, ready to Start.
type App struct {
	Engine *gin.Engine
	Config config.Config

	srv *http.Server
}

// Assemble wires the service identified by name (reported by /healthz) on
// defaultPort unless the config overrides it: config, database, dependency
// pingers, then routes via register. It does not bind the port.
func Assemble(name, defaultPort string, register func(*gin.Engine, Deps), opts ...Option) (*App, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	cfg := config.Load(name, defaultPort)
	if o.cfg != nil {
		cfg = *o.cfg
	}
	if cfg.JWTSecretInsecure {
		if cfg.JWTSecretRequired {
			return nil, errors.New("JWT_SECRET 未设置且 JWT_SECRET_REQUIRED 已开启:拒绝使用内置开发密钥启动")
		}
		log.Printf("WARNING: JWT_SECRET 未设置,正在使用内置开发密钥;跨服务令牌校验与公网部署必须设置 JWT_SECRET")
	}

	db := o.db
	if db == nil {
		var err error
		db, err = health.OpenMySQL(cfg.MysqlDSN)
		if err != nil {
			return nil, err
		}
	}

	pingers := o.pingers
	if pingers == nil {
		pingers = map[string]health.Pinger{
			"mysql": health.MySQL{DB: db},
			"redis": health.Redis{Client: health.NewRedis(cfg.RedisAddr)},
		}
	}

	r := server.New(cfg.Name, pingers)
	if register != nil {
		register(r, Deps{Config: cfg, DB: db})
	}
	var handler http.Handler = r
	if o.wrap != nil {
		handler = o.wrap(r)
	}
	return &App{Engine: r, Config: cfg, srv: &http.Server{Handler: handler}}, nil
}

// Start binds the configured port and serves in the background. Port
// conflicts surface synchronously: a desktop app must fail loudly rather
// than silently drifting to another port its SDK integrations don't know.
func (a *App) Start() error {
	ln, err := net.Listen("tcp", ":"+a.Config.Port)
	if err != nil {
		return err
	}
	log.Printf("%s listening on :%s", a.Config.Name, a.Config.Port)
	go func() {
		if err := a.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("%s: serve: %v", a.Config.Name, err)
		}
	}()
	return nil
}

// Stop gracefully shuts the HTTP server down.
func (a *App) Stop(ctx context.Context) error {
	return a.srv.Shutdown(ctx)
}

// Run boots the service identified by name and only returns on failure.
func Run(name, defaultPort string, register func(*gin.Engine, Deps)) {
	a, err := Assemble(name, defaultPort, register)
	if err != nil {
		log.Fatal(err)
	}
	if err := a.Start(); err != nil {
		log.Fatal(err)
	}
	select {} // serve runs in a background goroutine; block forever
}
