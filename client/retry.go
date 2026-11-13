package client

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRetryAfter = 3 * time.Second

func backoff(attempt int) time.Duration {
	delay := 100 * time.Millisecond << uint(attempt)
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	return delay + time.Duration(rand.IntN(100))*time.Millisecond
}

// retryAfter reports whether resp is retryable. A zero delay means the caller
// should use exponential backoff.
// retryAfter 判断 resp 是否可重试。返回零延迟表示调用方应使用指数退避。
func retryAfter(resp *http.Response) (time.Duration, bool) {
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return 0, true
	case http.StatusTooManyRequests:
		value := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if value == "" {
			return 0, false
		}
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 0 || time.Duration(seconds)*time.Second > maxRetryAfter {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	return 0, false
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
