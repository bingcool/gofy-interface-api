package docsui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bingcool/gofy-interface-api/docsui"
)

var sampleSpec = []byte(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{}}`)

func TestHandlerServesSpecAndUI(t *testing.T) {
	handler, err := docsui.Handler(docsui.Config{
		Title:   "Order API",
		Spec:    sampleSpec,
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("spec status = %d", rec.Code)
	}
	if rec.Body.String() != string(sampleSpec) {
		t.Fatalf("spec body = %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("docs status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Order API") {
		t.Fatalf("docs body does not contain the title")
	}
}

func TestHandlerBasicAuth(t *testing.T) {
	handler, err := docsui.Handler(docsui.Config{
		Spec:      sampleSpec,
		Enabled:   true,
		BasicAuth: &docsui.BasicAuth{Username: "docs", Password: "secret"},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate")
	}

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	req.SetBasicAuth("docs", "secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data, _ := io.ReadAll(rec.Body)
	if string(data) != string(sampleSpec) {
		t.Fatalf("body = %q", data)
	}
}

func TestDisabledHandler(t *testing.T) {
	handler, err := docsui.Handler(docsui.Config{Spec: sampleSpec})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestEmptySpecIsRejected(t *testing.T) {
	if _, err := docsui.Handler(docsui.Config{Enabled: true}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestPathNormalization(t *testing.T) {
	cfg := docsui.Config{DocsPath: "apidocs/", SpecPath: "spec.yaml"}
	if got := cfg.EffectiveDocsPath(); got != "/apidocs" {
		t.Fatalf("docs path = %q", got)
	}
	if got := cfg.EffectiveSpecPath(); got != "/spec.yaml" {
		t.Fatalf("spec path = %q", got)
	}
}
