// Command gofy-openapi validates or serves an existing OpenAPI 3.0.3 document.
// Generation belongs to the project module through docgen.Command, so this
// binary never imports a project's appinterface package.
// 命令 gofy-openapi 校验或托管已有的 OpenAPI 3.0.3 规范。
// 生成能力由项目模块通过 docgen.Command 提供，本命令不 import 任何项目的
// appinterface 包。
package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bingcool/gofy-interface-api/docgen"
	"github.com/bingcool/gofy-interface-api/docsui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gofy-openapi:", err)
		os.Exit(1)
	}
}

func run() error {
	flags := flag.NewFlagSet("gofy-openapi", flag.ContinueOnError)
	validate := flags.String("validate", "", "validate an existing OpenAPI file and exit")
	serve := flags.String("serve", "", "serve Swagger UI at this address, e.g. :8090")
	spec := flags.String("spec", "", "OpenAPI file to serve (required with -serve)")
	title := flags.String("title", "API Documentation", "document title")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return err
	}
	if *validate != "" && *serve == "" {
		return docgen.ValidateFile(*validate)
	}
	if *serve == "" {
		return errors.New("use -validate <file> or -serve <addr> -spec <file>")
	}
	if *spec == "" {
		return errors.New("-serve requires -spec <file>")
	}
	if err := docgen.ValidateFile(*spec); err != nil {
		return err
	}
	data, err := os.ReadFile(*spec)
	if err != nil {
		return fmt.Errorf("read %s: %w", *spec, err)
	}
	return serveDocs(*serve, *title, data)
}

func serveDocs(addr, title string, spec []byte) error {
	handler, err := docsui.Handler(docsui.Config{
		Title:   title,
		Spec:    spec,
		Enabled: true,
	})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/", handler)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("serving Swagger UI on http://localhost%s/docs (spec at /openapi.json)\n", addr)
	return server.ListenAndServe()
}
