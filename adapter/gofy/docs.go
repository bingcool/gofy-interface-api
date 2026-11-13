package gofyadapter

import (
	"github.com/bingcool/gofy-interface-api/docsui"
	"github.com/bingcool/gofy/support"
	"github.com/gin-gonic/gin"
)

type docsModule struct {
	cfg docsui.Config
}

// DocsModule mounts the Swagger UI and OpenAPI document as a gofy module.
// Register it only outside production or behind an internal gateway.
// DocsModule 以 gofy module 形式挂载 Swagger UI 与 OpenAPI 文档。
// 只应在非生产环境或内网网关之后注册。
func DocsModule(cfg docsui.Config) support.Module {
	return &docsModule{cfg: cfg}
}

func (m *docsModule) Name() string { return "docs" }

func (m *docsModule) Register(r *support.Router) error {
	// Registering the module is the opt-in; make docsui.Config's zero value work.
	// 注册模块本身即表示启用，因此让 docsui.Config 的零值也能工作。
	m.cfg.Enabled = true
	h, err := docsui.Handler(m.cfg)
	if err != nil {
		return err
	}
	docsPath := m.cfg.EffectiveDocsPath()
	r.GET(docsPath, gin.WrapH(h))
	if docsPath != "/" {
		// Swagger UI loads its assets from DocsPath/.
		// Swagger UI 会从 DocsPath/ 下加载静态资源。
		r.GET(docsPath+"/*any", gin.WrapH(h))
	}
	r.GET(m.cfg.EffectiveSpecPath(), gin.WrapH(h))
	return nil
}
