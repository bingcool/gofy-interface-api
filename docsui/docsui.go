// Package docsui serves an OpenAPI document and an embedded Swagger UI.
//
// It depends only on net/http, so it can be mounted in any server, not just
// gofy. The assets come from github.com/swaggest/swgui/v5emb and are compiled
// into the binary; no CDN is required.
// 包 docsui 提供 OpenAPI 文档与内嵌 Swagger UI 的 HTTP 处理器。它只依赖
// net/http，因此可以挂到任意服务器，而不限于 gofy。静态资源来自
// github.com/swaggest/swgui/v5emb 并编译进二进制，不需要 CDN。
package docsui

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/swaggest/swgui/v5emb"
)

// BasicAuth protects the documentation endpoints.
// BasicAuth 是保护文档端点的基本认证凭据。
type BasicAuth struct {
	Username string
	Password string
}

// Config configures Handler.
// Config 是 Handler 的配置。
type Config struct {
	// Title is shown in the Swagger UI top bar.
	// Title 显示在 Swagger UI 顶栏，为空时使用 API Documentation。
	Title string

	// Spec is the OpenAPI document (JSON or YAML).
	// Spec 是 OpenAPI 文档内容，支持 JSON 或 YAML；为空时 Handler 返回错误。
	Spec []byte

	// SpecPath defaults to /openapi.json.
	// SpecPath 是原始规范的暴露路径，默认为 /openapi.json。
	SpecPath string

	// DocsPath defaults to /docs.
	// DocsPath 是 Swagger UI 的挂载路径，默认为 /docs。
	DocsPath string

	// Enabled must be true for Handler to serve anything. DocsModule sets it
	// for you, because mounting the module is already an explicit opt-in.
	// Enabled 必须为 true，Handler 才会提供任何内容；DocsModule 会自动置位，
	// 因为挂载该模块本身已经是显式启用。
	Enabled bool

	// BasicAuth is optional.
	// BasicAuth 可选，非空时对所有文档端点启用 HTTP 基本认证。
	BasicAuth *BasicAuth
}

// EffectiveDocsPath returns the normalized docs path.
// EffectiveDocsPath 返回归一化后的文档路径：补前导斜杠、去掉尾部斜杠。
func (c Config) EffectiveDocsPath() string {
	return normalizePath(c.DocsPath, "/docs")
}

// EffectiveSpecPath returns the normalized spec path.
// EffectiveSpecPath 返回归一化后的规范路径：补前导斜杠、去掉尾部斜杠。
func (c Config) EffectiveSpecPath() string {
	return normalizePath(c.SpecPath, "/openapi.json")
}

// Handler builds the documentation handler. It serves the UI at DocsPath
// (including its assets under DocsPath/) and the raw spec at SpecPath.
// Handler 构建文档处理器：在 DocsPath 提供 UI（静态资源在 DocsPath/ 下），在
// SpecPath 提供原始规范。Enabled 为 false 时返回 404 处理器；Spec 为空或两个
// 路径相同时返回错误。
func Handler(cfg Config) (http.Handler, error) {
	if !cfg.Enabled {
		return http.NotFoundHandler(), nil
	}
	if len(cfg.Spec) == 0 {
		return nil, errors.New("docsui: spec is empty")
	}
	title := strings.TrimSpace(cfg.Title)
	if title == "" {
		title = "API Documentation"
	}
	docsPath := cfg.EffectiveDocsPath()
	specPath := cfg.EffectiveSpecPath()
	if docsPath == specPath {
		return nil, errors.New("docsui: DocsPath and SpecPath must differ")
	}

	ui := v5emb.NewHandler(title, specPath, docsPath+"/")
	mux := http.NewServeMux()
	mux.Handle(specPath, specHandler(specPath, cfg.Spec))

	if docsPath == "/" {
		mux.Handle("/", ui)
	} else {
		mux.Handle(docsPath+"/", ui)
		mux.HandleFunc(docsPath, func(w http.ResponseWriter, r *http.Request) {
			req := r.Clone(r.Context())
			req.URL.Path = docsPath + "/"
			ui.ServeHTTP(w, req)
		})
	}

	var h http.Handler = mux
	if cfg.BasicAuth != nil {
		h = protect(h, cfg.BasicAuth)
	}
	return h, nil
}

func specHandler(path string, spec []byte) http.Handler {
	contentType := "application/json"
	if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
		contentType = "application/yaml"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType+"; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(spec)
	})
}

func protect(next http.Handler, auth *BasicAuth) http.Handler {
	wantUser := []byte(auth.Username)
	wantPass := []byte(auth.Password)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), wantUser) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), wantPass) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="docs", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		req := r.Clone(r.Context())
		req.Header = r.Header.Clone()
		req.Header.Del("Authorization")
		next.ServeHTTP(w, req)
	})
}

func normalizePath(path, fallback string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return fallback
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}
