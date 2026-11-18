package client_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bingcool/gofy-interface-api/client"
	"github.com/bingcool/gofy-interface-api/internal/testcontract"
)

// TestMissingDataIsProtocolErrorForValueResponse pins D5: a successful envelope
// without data cannot satisfy a value-typed response, and the old (nil, nil)
// handed callers a pointer they were about to dereference. Only a NoContent
// route, a NoBody placeholder route or a pointer response may succeed without
// a payload.
// TestMissingDataIsProtocolErrorForValueResponse 固定 D5：没有 data 的成功信封满足
// 不了值类型响应，旧行为 (nil, nil) 把即将被解引用的空指针交给调用方；只有
// NoContent 路由、NoBody 占位路由或指针响应才允许没有 payload 也成功。
func TestMissingDataIsProtocolErrorForValueResponse(t *testing.T) {
	cases := map[string]string{
		"absent": `{"code":0,"msg":"success","req_id":"d1"}`,
		"null":   `{"code":0,"msg":"success","data":null,"req_id":"d2"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()

			c := newFixtureClient(t, server.URL, client.Config{})
			_, err := client.Call(context.Background(), c, testcontract.GetItem, &testcontract.ItemRequest{ID: 1})
			var protocolErr *client.ProtocolError
			if !errors.As(err, &protocolErr) {
				t.Fatalf("err = %v, want ProtocolError", err)
			}
		})
	}
}

// TestMissingDataIsNilForNoBodyRoute checks the other side of D5: a route whose
// response type is the endpoint.NoBody placeholder still returns (nil, nil)
// when the envelope carries no data.
// TestMissingDataIsNilForNoBodyRoute 校验 D5 的另一侧：响应类型为 endpoint.NoBody
// 占位的路由在信封不带 data 时仍然返回 (nil, nil)。
func TestMissingDataIsNilForNoBodyRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":0,"msg":"success","req_id":"d3"}`)
	}))
	defer server.Close()

	c := newFixtureClient(t, server.URL, client.Config{})
	res, err := client.Call(context.Background(), c, testcontract.CancelItem, &testcontract.CancelRequest{ID: 1})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if res != nil {
		t.Fatalf("res = %+v, want nil", res)
	}
}
