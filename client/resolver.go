package client

import (
	"context"
	"errors"
	"strings"
)

// Resolver maps a service name to a base URL.
// Resolver 把服务名映射成基地址；每次调用都会询问它，因此实现负责缓存与选址。
type Resolver interface {
	Resolve(ctx context.Context, service string) (baseURL string, err error)
}

// StaticResolver always returns BaseURL. It is useful for tests and fixed
// deployments.
// StaticResolver 始终返回 BaseURL，适用于测试和地址固定的部署。
type StaticResolver struct {
	BaseURL string
}

// Resolve implements Resolver.
// Resolve 实现 Resolver 接口。
func (r StaticResolver) Resolve(context.Context, string) (string, error) {
	if strings.TrimSpace(r.BaseURL) == "" {
		return "", errors.New("client: StaticResolver.BaseURL is empty")
	}
	return r.BaseURL, nil
}
