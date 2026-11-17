package client

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/bingcool/gofy-interface-api/contracts/common/endpoint"
)

// uploadRequest is the fixture shape: a pointer FileHeader is how callers pass
// one file, and walkStruct handles it through asFileHeader.
// uploadRequest 是 fixture 形状：调用方用指针 FileHeader 传一个文件，walkStruct 通过
// asFileHeader 处理它。
type uploadRequest struct {
	File *multipart.FileHeader `form:"file"`
}

func multipartSpec() endpoint.Spec {
	return endpoint.Spec{Method: http.MethodPost, RequestContentType: "multipart/form-data"}
}

// fileHeader builds a real *multipart.FileHeader, because its Open method reads
// from an internal temp file that cannot be faked with a struct literal.
// fileHeader 构造真实的 *multipart.FileHeader：它的 Open 方法读内部临时文件，无法用
// 结构体字面量伪造。
func fileHeader(t *testing.T, name string, data []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	form, err := multipart.NewReader(&buf, writer.Boundary()).ReadForm(int64(len(data)) + 1<<20)
	if err != nil {
		t.Fatalf("read form: %v", err)
	}
	t.Cleanup(func() { _ = form.RemoveAll() })
	return form.File["file"][0]
}

// TestMultipartRespectsLimit is the P3-02 acceptance test: the body is built in
// memory, so the cap has to be enforced while writing, not after OOM. Exactly
// the limit succeeds; one byte less fails with an identifiable error.
// TestMultipartRespectsLimit 是 P3-02 的验收测试：请求体在内存里构建，所以上限必须在
// 写入过程中生效，而不是等到 OOM。恰好等于上限成功；少一个字节则给出可识别的错误。
func TestMultipartRespectsLimit(t *testing.T) {
	req := &uploadRequest{File: fileHeader(t, "a.bin", bytes.Repeat([]byte("a"), 4096))}
	spec := multipartSpec()

	full, contentType, err := encodeBody(spec, req, 1<<20)
	if err != nil {
		t.Fatalf("encodeBody under limit: %v", err)
	}
	if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
		t.Fatalf("content type = %q", contentType)
	}

	limit := int64(len(full))
	atLimit, _, err := encodeBody(spec, req, limit)
	if err != nil {
		t.Fatalf("encodeBody at limit %d: %v", limit, err)
	}
	if len(atLimit) != len(full) {
		t.Fatalf("body length = %d at limit, want %d", len(atLimit), len(full))
	}

	_, _, err = encodeBody(spec, req, limit-1)
	if !errors.Is(err, ErrMultipartTooLarge) {
		t.Fatalf("encodeBody over limit error = %v, want ErrMultipartTooLarge", err)
	}
	if !strings.Contains(err.Error(), "MaxMultipartBytes") {
		t.Fatalf("error %q does not name the setting", err)
	}
}

// TestMultipartLimitIsPerRequest checks the cap is not a client-wide budget:
// two concurrent uploads below the limit must both succeed even though their
// combined size exceeds it.
// TestMultipartLimitIsPerRequest 验证上限不是客户端级配额：两个各自低于上限的并发上传
// 必须都成功，即使它们加起来超过上限。
func TestMultipartLimitIsPerRequest(t *testing.T) {
	const size = 3 << 20
	// Build the fixtures on the test goroutine: fileHeader calls t.Fatalf, which
	// is not safe from a spawned goroutine.
	// fixture 在测试 goroutine 上构造：fileHeader 会调用 t.Fatalf，在子 goroutine 中
	// 不安全。
	reqs := make([]*uploadRequest, 2)
	for i := range reqs {
		reqs[i] = &uploadRequest{File: fileHeader(t, "part.bin", bytes.Repeat([]byte("b"), size))}
	}

	var wg sync.WaitGroup
	errs := make([]error, len(reqs))
	for i := range reqs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _, errs[idx] = encodeBody(multipartSpec(), reqs[idx], 5<<20)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("upload %d under a per-request limit failed: %v", i, err)
		}
	}
}

func TestMultipartLimitDefaults(t *testing.T) {
	c, err := New(Config{BaseURL: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c.maxMultipart != defaultMaxMultipartBytes {
		t.Fatalf("maxMultipart = %d, want default %d", c.maxMultipart, defaultMaxMultipartBytes)
	}

	if _, err := New(Config{BaseURL: "http://127.0.0.1:9", MaxMultipartBytes: -1}); err == nil {
		t.Fatal("negative MaxMultipartBytes was accepted")
	}
}
