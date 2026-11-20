package types_test

import (
	"encoding/json"
	"testing"

	"github.com/bingcool/gofy-interface-api/contracts/common/types"
)

// jsonNull is what a client sends for an absent optional field. Each type has to
// accept it and land on the zero value: a 400 for "date": null is a contract gap,
// not a validation win.
// jsonNull 是客户端对"可选字段没有值"的写法。三种类型都必须接受它并落到零值：
// 对 "date": null 返回 400 是契约缺口，不是校验上的胜利。
func TestJSONNullBecomesZeroValue(t *testing.T) {
	t.Run("date", func(t *testing.T) {
		var d types.Date
		if err := json.Unmarshal([]byte(`null`), &d); err != nil {
			t.Fatalf("unmarshal null date: %v", err)
		}
		if !d.IsZero() {
			t.Fatalf("date = %v, want the zero value", d.Time)
		}
	})

	t.Run("uint64 string", func(t *testing.T) {
		v := types.Uint64String(7)
		if err := json.Unmarshal([]byte(`null`), &v); err != nil {
			t.Fatalf("unmarshal null uint64: %v", err)
		}
		if v != 0 {
			t.Fatalf("uint64 = %d, want 0", v)
		}
	})

	t.Run("int64 string", func(t *testing.T) {
		v := types.Int64String(-7)
		if err := json.Unmarshal([]byte(`null`), &v); err != nil {
			t.Fatalf("unmarshal null int64: %v", err)
		}
		if v != 0 {
			t.Fatalf("int64 = %d, want 0", v)
		}
	})
}

// TestJSONValuesStillRoundTrip guards the null branch against swallowing real
// values: the accepted forms keep working, and malformed input still errors.
// TestJSONValuesStillRoundTrip 防止 null 分支吞掉真实取值：原有写法保持可用，非法
// 入参仍然报错。
func TestJSONValuesStillRoundTrip(t *testing.T) {
	var d types.Date
	if err := json.Unmarshal([]byte(`"2026-11-20"`), &d); err != nil {
		t.Fatalf("unmarshal date: %v", err)
	}
	if got := d.Format("2006-01-02"); got != "2026-11-20" {
		t.Fatalf("date = %q, want 2026-11-20", got)
	}
	if err := json.Unmarshal([]byte(`"2026-13-01"`), &d); err == nil {
		t.Fatal("invalid date was accepted")
	}

	var u types.Uint64String
	if err := json.Unmarshal([]byte(`"18446744073709551615"`), &u); err != nil {
		t.Fatalf("unmarshal uint64 string: %v", err)
	}
	if uint64(u) != ^uint64(0) {
		t.Fatalf("uint64 = %d, want max", uint64(u))
	}
	if err := json.Unmarshal([]byte(`"not-a-number"`), &u); err == nil {
		t.Fatal("invalid uint64 was accepted")
	}

	var i types.Int64String
	if err := json.Unmarshal([]byte(`-9223372036854775808`), &i); err != nil {
		t.Fatalf("unmarshal int64 number: %v", err)
	}
	if int64(i) != -9223372036854775808 {
		t.Fatalf("int64 = %d, want min", int64(i))
	}
	if err := json.Unmarshal([]byte(`null`), &i); err != nil || i != 0 {
		t.Fatalf("int64 after null = %d, %v", i, err)
	}
}
