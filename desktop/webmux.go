package main

import (
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// originMux turns one loopback origin into a full SPA host: the API engine
// keeps its root-level routes (/v1, /admin, /canvases, /assets/…), the SPA
// bundle answers everything else with history-mode fallback to index.html,
// and /api-style aliases map onto backends exactly like the vite dev proxy
// and the nginx deployment do:
//
//	gateway origin: /api/* → self (prefix stripped), /canvas-api/* → canvas
//	canvas origin:  /api/* → self (prefix stripped)
//
// The engine is tried first with a 404-swallowing writer so engine routes
// always win over the SPA fallback (e.g. canvas /assets/{id}/content vs the
// vite dist's own /assets/*.js directory), and streaming responses (SSE on
// /v1, video content) pass through untouched once the status is decided.
type originMux struct {
	engine  http.Handler
	spa     http.Handler
	aliases []aliasRoute
}

type aliasRoute struct {
	prefix string
	// handler nil means "same engine, prefix stripped".
	handler http.Handler
}

// newOriginMux wires an origin. engine serves the API; spaFS is the built
// SPA bundle (may be a placeholder when the frontend isn't built yet);
// extraAliases adds prefix-forwarded routes beyond the /api self-alias.
func newOriginMux(engine http.Handler, spaFS fs.FS, extraAliases map[string]http.Handler) *originMux {
	m := &originMux{
		engine: engine,
		spa:    spaHandler(spaFS),
		aliases: []aliasRoute{
			{prefix: "/api"},
		},
	}
	for prefix, handler := range extraAliases {
		m.aliases = append(m.aliases, aliasRoute{prefix: prefix, handler: handler})
	}
	return m
}

func (m *originMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	for _, a := range m.aliases {
		if p == a.prefix || strings.HasPrefix(p, a.prefix+"/") {
			rewritten := r.Clone(r.Context())
			rewritten.URL.Path = stripPrefix(p, a.prefix)
			if a.handler != nil {
				a.handler.ServeHTTP(w, rewritten)
			} else {
				m.engine.ServeHTTP(w, rewritten)
			}
			return
		}
	}

	fw := &fallbackWriter{ResponseWriter: w}
	m.engine.ServeHTTP(fw, r)
	if fw.decided && fw.status == http.StatusNotFound {
		// gin 的 404 路径已把 Content-Type: text/plain 写进响应头,
		// 而 FileServer 见头里已有 Content-Type 就不再覆盖 —— 不清掉,
		// index.html / JS 会被当纯文本发出去(webview 直接显示源码)。
		w.Header().Del("Content-Type")
		m.spa.ServeHTTP(w, r)
	}
}

func stripPrefix(p, prefix string) string {
	rest := strings.TrimPrefix(p, prefix)
	if rest == "" {
		return "/"
	}
	return rest
}

// spaHandler serves the built bundle with SPA history fallback: existing
// files (and directory indexes) win, everything else falls back to the
// bundle's index.html; a not-yet-built bundle gets a placeholder page.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	indexPresent := func() bool {
		f, err := fsys.Open("index.html")
		if err != nil {
			return false
		}
		f.Close()
		return true
	}()
	if !indexPresent {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = w.Write([]byte(placeholderHTML))
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
			if f, err := fsys.Open(p); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		rewritten := r.Clone(r.Context())
		rewritten.URL.Path = "/"
		fileServer.ServeHTTP(w, rewritten)
	})
}

// proxyTo forwards a stripped-prefix alias to another loopback service,
// the desktop counterpart of nginx's /canvas-api location.
func proxyTo(target *url.URL) http.Handler {
	return httputil.NewSingleHostReverseProxy(target)
}

// fallbackWriter defers the header write so an engine 404 can be swapped
// for the SPA fallback without re-sending headers; every non-404 response
// (including SSE/video streams) is forwarded as-is.
type fallbackWriter struct {
	http.ResponseWriter
	status  int
	decided bool
}

func (fw *fallbackWriter) decide(code int) {
	if fw.decided {
		return
	}
	fw.decided = true
	fw.status = code
	if code != http.StatusNotFound {
		fw.ResponseWriter.WriteHeader(code)
	}
}

func (fw *fallbackWriter) WriteHeader(code int) { fw.decide(code) }

func (fw *fallbackWriter) Write(b []byte) (int, error) {
	fw.decide(http.StatusOK)
	if fw.status == http.StatusNotFound {
		return len(b), nil // swallowed: the SPA fallback answers instead
	}
	return fw.ResponseWriter.Write(b)
}

// Flush keeps streaming responses (SSE relay, progressive video) live.
func (fw *fallbackWriter) Flush() {
	fw.decide(http.StatusOK)
	if fw.status == http.StatusNotFound {
		return
	}
	if f, ok := fw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

const placeholderHTML = `<!doctype html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>InfiniteChance</title>
<style>body{font-family:-apple-system,sans-serif;display:grid;place-items:center;height:100vh;margin:0;color:#444}main{text-align:center;line-height:1.8}</style>
</head>
<body><main><h1>前端资源尚未构建</h1>
<p>运行 <code>make desktop-frontend</code> 构建两个 SPA 后重启应用。</p></main></body>
</html>`
