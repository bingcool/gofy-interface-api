// Command refcheck enforces the import boundaries of gofy-interface-api and of
// a project's contract module.
//
// In library mode each directory is checked against the rules in §4.3. The
// library is framework independent: gofy and gin are denied everywhere, because
// the gofy adapter and the Nacos resolver now live in gofy-adapter.
// contracts may import only the standard library and contracts/**; client/**
// additionally allows contracts/** and client/**; docgen is limited to
// contracts, kin-openapi and yaml; docsui is limited to swgui; cmd may import
// anything inside the library. No Go file may declare init().
//
// In contracts mode -app is checked against the project module: only the
// standard library, the project's own appinterface/**, and the api library's
// contracts/** and client/** are allowed. Frameworks, adapter and docgen stay
// out of the business contract layer, and init() is forbidden as well.
// 命令 refcheck 校验 gofy-interface-api 与项目契约模块的分层边界。
//
// library 模式按 §4.3 逐目录检查。本库与框架无关：gofy 与 gin 在全库范围内
// 禁止，因为 gofy adapter 与 Nacos resolver 已迁至 gofy-adapter。
// contracts 只允许标准库与 contracts/**；client 增加 contracts/** 与 client/**；
// docgen 限于 contracts、kin-openapi 与 yaml；docsui 限于 swgui；cmd 可 import
// 库内任意包。任何文件都不得声明 init()。
//
// contracts 模式检查 -app：只允许标准库、项目自身 appinterface/** 以及 api 库
// 的 contracts/** 与 client/**；框架、adapter 与 docgen 不得进入业务契约层，
// 同样禁止 init()。
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultAPIModule = "github.com/bingcool/gofy-interface-api"

// forbiddenFrameworkImports keeps the contract library independent of gofy.
// A prefix comparison with a trailing slash avoids matching the api library's
// own module path, which starts with "github.com/bingcool/gofy-interface-api".
// forbiddenFrameworkImports 保证契约库不依赖 gofy。前缀比较带尾部斜杠，
// 避免误伤以 github.com/bingcool/gofy-interface-api 开头的本库路径。
var forbiddenFrameworkImports = []string{
	"github.com/bingcool/gofy",
	"github.com/gin-gonic/gin",
}

const adapterModule = "github.com/bingcool/gofy-adapter"

type rule struct {
	name      string
	dirs      []string
	skipDirs  []string
	allow     []string
	deny      []string
	allowAll  bool
	checkInit bool
	testAllow []string
}

func main() {
	mode := flag.String("mode", "library", "check mode: library or contracts")
	dir := flag.String("dir", ".", "library root (library mode)")
	app := flag.String("app", "appinterface", "project appinterface directory (contracts mode)")
	module := flag.String("module", "github.com/yourorg/interface-service", "project module path (contracts mode)")
	api := flag.String("api", defaultAPIModule, "gofy-interface-api module path")
	flag.Parse()

	var rules []rule
	switch *mode {
	case "library":
		rules = libraryRules(*dir, *api)
	case "contracts":
		rules = contractsRules(*app, *module, *api)
	default:
		fmt.Fprintf(os.Stderr, "refcheck: unknown mode %q\n", *mode)
		os.Exit(2)
	}

	var problems []string
	for _, r := range rules {
		found, err := checkRule(r)
		if err != nil {
			fmt.Fprintln(os.Stderr, "refcheck:", err)
			os.Exit(2)
		}
		problems = append(problems, found...)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		for _, problem := range problems {
			fmt.Fprintln(os.Stderr, "refcheck:", problem)
		}
		os.Exit(1)
	}
	fmt.Printf("refcheck: %s mode boundaries are clean\n", *mode)
}

func libraryRules(root, api string) []rule {
	join := func(p string) string { return filepath.Join(root, p) }
	rs := []rule{
		{
			name: "contracts", dirs: []string{join("contracts")},
			allow: []string{api + "/contracts"}, checkInit: true,
		},
		{
			name: "client", dirs: []string{join("client")},
			allow: []string{api + "/contracts", api + "/client"}, checkInit: true,
		},
		{
			name: "docgen", dirs: []string{join("docgen")},
			allow:     []string{api + "/contracts", "github.com/getkin/kin-openapi", "gopkg.in/yaml.v3"},
			checkInit: true,
		},
		{
			name: "docsui", dirs: []string{join("docsui")},
			allow: []string{"github.com/swaggest/swgui"}, checkInit: true,
		},
		{
			name: "cmd", dirs: []string{join("cmd")}, allowAll: true, checkInit: true,
		},
		{
			name: "internal", dirs: []string{join("internal")},
			allow: []string{api + "/contracts", api + "/client"}, checkInit: true,
		},
	}
	for i := range rs {
		rs[i].testAllow = []string{api}
		rs[i].deny = forbiddenFrameworkImports
	}
	return rs
}

func contractsRules(appDir, module, api string) []rule {
	return []rule{{
		name: "appinterface", dirs: []string{appDir},
		allow:     []string{module + "/appinterface", api + "/contracts", api + "/client"},
		checkInit: true,
	}}
}

func checkRule(r rule) ([]string, error) {
	var problems []string
	fset := token.NewFileSet()
	for _, dir := range r.dirs {
		info, err := os.Stat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", dir)
		}
		err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || isSkipped(path, r.skipDirs) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: parse: %v", path, err))
				return nil
			}
			er := r
			if strings.HasSuffix(path, "_test.go") {
				er.allow = append(append([]string{}, r.allow...), r.testAllow...)
			}
			for _, spec := range file.Imports {
				importPath := strings.Trim(spec.Path.Value, `"`)
				if isDenied(importPath, er.deny) {
					problems = append(problems, fmt.Sprintf("%s: %s must not import framework %q", path, r.name, importPath))
					continue
				}
				if !allowed(importPath, er) {
					problems = append(problems, fmt.Sprintf("%s: forbidden import %q in %s", path, importPath, r.name))
				}
			}
			if r.checkInit {
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if ok && fn.Recv == nil && fn.Name.Name == "init" {
						problems = append(problems, fmt.Sprintf("%s: %s must not declare init()", path, r.name))
					}
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return problems, nil
}

func isSkipped(path string, skips []string) bool {
	clean := filepath.Clean(path)
	for _, s := range skips {
		if clean == filepath.Clean(s) {
			return true
		}
	}
	return false
}

func isDenied(importPath string, deny []string) bool {
	for _, prefix := range deny {
		if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
			return true
		}
	}
	return false
}

func allowed(importPath string, r rule) bool {
	if r.allowAll {
		return true
	}
	for _, prefix := range r.allow {
		if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
			return true
		}
	}
	first := importPath
	if i := strings.IndexByte(first, '/'); i >= 0 {
		first = first[:i]
	}
	return !strings.Contains(first, ".")
}
