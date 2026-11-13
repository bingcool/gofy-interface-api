package docgen

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
	"github.com/getkin/kin-openapi/openapi3"
)

type prefixFlags map[string]string

func (p prefixFlags) String() string {
	if len(p) == 0 {
		return ""
	}
	parts := make([]string, 0, len(p))
	for key, value := range p {
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, ",")
}

func (p prefixFlags) Set(value string) error {
	key, val, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return fmt.Errorf("prefix must look like service=/path, got %q", value)
	}
	p[strings.TrimSpace(key)] = strings.TrimSpace(val)
	return nil
}

// Command runs the project-side OpenAPI generator. Routes come from the
// caller's appinterface package; defaults supply the document metadata and
// deployment prefixes, and flags override them.
//
// -validate only checks an existing document and ignores routes. Generation
// requires -all or -service; prefixes are merged per key so a command-line
// override keeps the other entries supplied by defaults. This function lives
// in the library and never imports a project contract package.
// Command 运行项目侧的 OpenAPI 生成器。路由由调用方的 appinterface 提供，
// defaults 提供文档元数据与部署前缀，命令行参数可覆盖。
//
// -validate 只校验已有文档并忽略 routes；生成必须指定 -all 或 -service；
// -prefix 按 key 合并，命令行覆盖不会丢掉 defaults 里的其他前缀。本函数位于
// 库内，不 import 任何项目契约包。
func Command(routes []endpoint.Descriptor, defaults Config) error {
	flags := flag.NewFlagSet("openapi", flag.ContinueOnError)
	all := flags.Bool("all", false, "generate every contract module")
	service := flags.String("service", "", "generate one service, e.g. demoservice")
	outDir := flags.String("out", "./openapi", "output directory")
	validate := flags.String("validate", "", "validate an existing OpenAPI file and exit")
	title := flags.String("title", "", "override document title")
	version := flags.String("version", "", "override document version")
	description := flags.String("description", "", "override document description")
	prefixes := prefixFlags{}
	flags.Var(prefixes, "prefix", "deployment prefix, repeatable: -prefix service=/path")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}

	if *validate != "" {
		return ValidateFile(*validate)
	}

	selected := routes
	name := "all"
	if *service != "" {
		selected = FilterByService(routes, *service)
		if len(selected) == 0 {
			return fmt.Errorf("no routes match service %q", *service)
		}
		name = *service
	} else if !*all {
		return errors.New("use -all or -service to select routes")
	}

	cfg := defaults
	if *title != "" {
		cfg.Title = *title
	}
	if *version != "" {
		cfg.Version = *version
	}
	if *description != "" {
		cfg.Description = *description
	}
	cfg.Prefixes = mergePrefixes(defaults.Prefixes, prefixes)

	spec, err := Build(selected, cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := spec.Validate(ctx); err != nil {
		return fmt.Errorf("validate generated spec: %w", err)
	}
	yamlData, err := spec.YAML()
	if err != nil {
		return err
	}
	jsonData, err := spec.JSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	yamlPath := filepath.Join(*outDir, name+".yaml")
	jsonPath := filepath.Join(*outDir, name+".json")
	if err := os.WriteFile(yamlPath, yamlData, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", yamlPath, err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", jsonPath, err)
	}
	fmt.Printf("wrote %s and %s (%d routes)\n", yamlPath, jsonPath, len(selected))
	return nil
}

// ValidateFile loads and validates an existing OpenAPI document.
// ValidateFile 加载并校验已有的 OpenAPI 文档。
func ValidateFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return fmt.Errorf("load %s: %w", path, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := doc.Validate(ctx); err != nil {
		return fmt.Errorf("validate %s: %w", path, err)
	}
	fmt.Println("valid:", path)
	return nil
}

func mergePrefixes(defaults, overrides map[string]string) map[string]string {
	if len(defaults) == 0 && len(overrides) == 0 {
		return nil
	}
	merged := make(map[string]string, len(defaults)+len(overrides))
	for key, value := range defaults {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}
