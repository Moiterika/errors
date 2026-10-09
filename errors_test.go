package errors_test

import (
	stderrors "errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"testing"

	"github.com/Moiterika/errors"
)

var errSentinel = errors.New("sentinel")

type myError struct{ code int }

func (e *myError) Error() string { return fmt.Sprintf("my error %d", e.code) }

// 標準の errors / fmt.Errorf と出力が一致すること。
func TestMessageCompatibility(t *testing.T) {
	base := stderrors.New("base")
	other := stderrors.New("other")

	tests := []struct {
		name string
		got  error
		want error
	}{
		{"New", errors.New("msg"), stderrors.New("msg")},
		{"Errorf no wrap", errors.Errorf("n=%d", 1), fmt.Errorf("n=%d", 1)},
		{"Errorf suffix %w", errors.Errorf("ctx: %w", base), fmt.Errorf("ctx: %w", base)},
		{"Errorf middle %w", errors.Errorf("a %w b", base), fmt.Errorf("a %w b", base)},
		{"Errorf multi %w", errors.Errorf("%w + %w", base, other), fmt.Errorf("%w + %w", base, other)},
		{"Errorf indexed %w", errors.Errorf("%[2]w: %[1]s", "x", base), fmt.Errorf("%[2]w: %[1]s", "x", base)},
		{"Errorf %w nil", errors.Errorf("ctx: %w", nil), fmt.Errorf("ctx: %w", nil)},
		{"Join", errors.Join(base, nil, other), stderrors.Join(base, nil, other)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if g, w := tt.got.Error(), tt.want.Error(); g != w {
				t.Errorf("Error() = %q, want %q", g, w)
			}
			for _, verb := range []string{"%v", "%s", "%q", "%10.4s", "%x"} {
				if g, w := fmt.Sprintf(verb, tt.got), fmt.Sprintf(verb, tt.want); g != w {
					t.Errorf("Sprintf(%s) = %q, want %q", verb, g, w)
				}
			}
		})
	}
}

func TestJoinNil(t *testing.T) {
	if err := errors.Join(); err != nil {
		t.Errorf("Join() = %v, want nil", err)
	}
	if err := errors.Join(nil, nil); err != nil {
		t.Errorf("Join(nil, nil) = %v, want nil", err)
	}
}

func TestIsAs(t *testing.T) {
	my := &myError{code: 42}
	err := errors.Errorf("outer: %w", errors.Errorf("inner: %w", errors.Join(fs.ErrNotExist, my)))
	multi := errors.Errorf("%w and %w", errSentinel, fs.ErrPermission)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("Is(err, fs.ErrNotExist) = false")
	}
	if !stderrors.Is(err, fs.ErrNotExist) {
		t.Error("標準の errors.Is でもたどれること")
	}
	if !errors.Is(multi, errSentinel) || !errors.Is(multi, fs.ErrPermission) {
		t.Error("複数 %w のどちらにも Is がマッチすること")
	}
	if errors.Is(err, errSentinel) {
		t.Error("Is(err, errSentinel) = true")
	}

	var target *myError
	if !errors.As(err, &target) || target.code != 42 {
		t.Errorf("As = %v, want code 42", target)
	}
	if got, ok := errors.AsType[*myError](err); !ok || got != my {
		t.Errorf("AsType = %v, %v", got, ok)
	}

	if got := errors.Unwrap(errors.Errorf("ctx: %w", errSentinel)); got != errSentinel {
		t.Errorf("Unwrap = %v, want errSentinel", got)
	}
	if errors.ErrUnsupported != stderrors.ErrUnsupported {
		t.Error("ErrUnsupported が標準と異なる")
	}
}

func TestStackTrace(t *testing.T) {
	inner := errors.New("root cause") // line A
	outer := errors.Errorf("load config: %w", inner)
	got := fmt.Sprintf("%+v", outer)
	t.Logf("\n%s", got)

	for _, want := range []string{
		"load config:",
		"root cause",
		"_test.TestStackTrace",
		"errors_test.go:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("%%+v に %q が含まれない", want)
		}
	}
	// 2箇所 (New と Errorf) の位置が記録されていること。
	if n := strings.Count(got, "errors_test.go:"); n != 2 {
		t.Errorf("フレーム数 = %d, want 2", n)
	}
	// 外側のメッセージが内側のメッセージを重複して含まないこと。
	if n := strings.Count(got, "root cause"); n != 1 {
		t.Errorf("\"root cause\" の出現回数 = %d, want 1", n)
	}
}

// line は呼び出した行の行番号を返す。
func line() int {
	_, _, l, _ := runtime.Caller(1)
	return l
}

// 記録した位置が New / Errorf / Join を呼んだ行と一致すること。
func TestStackTraceLine(t *testing.T) {
	check := func(name string, err error, line int) {
		t.Helper()
		got := fmt.Sprintf("%+v", err)
		// 行番号の後ろの改行は、「:13」が「:130」に一致しないようにするため。最後の行には改行がないので補う。
		if want := fmt.Sprintf("errors_test.go:%d\n", line); !strings.Contains(got+"\n", want) {
			t.Errorf("%s: %%+v に %q が含まれない:\n%s", name, want, got)
		}
	}

	err, l := errors.New("new"), line()
	check("New", err, l)
	err, l = errors.Errorf("errorf: %w", errSentinel), line()
	check("Errorf", err, l)
	err, l = errors.Join(errSentinel), line()
	check("Join", err, l)
}

// Join と複数の %w では、枝ごとに [0], [1], ... の番号が付くこと。
func TestStackTraceBranchIndex(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string // 出力に現れる順
	}{
		{"Join", errors.Join(errors.New("first"), errors.New("second")), []string{"[0] first", "[1] second"}},
		{"Errorf multi %w", errors.Errorf("%w and %w", errors.New("a"), errors.New("b")), []string{"[0] a", "[1] b"}},
		{"nested Join", errors.Join(errors.New("x"), errors.Join(errors.New("y"))), []string{"[0] x", "[1] joined errors", "[0] y"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmt.Sprintf("%+v", tt.err)
			rest := got
			for _, w := range tt.want {
				i := strings.Index(rest, w)
				if i < 0 {
					t.Fatalf("%%+v に %q が順番どおりに含まれない:\n%s", w, got)
				}
				rest = rest[i+len(w):]
			}
		})
	}
}

func TestStackTraceMulti(t *testing.T) {
	err := errors.Join(
		errors.New("first"),
		errors.Errorf("second: %w and %w", errors.New("x"), stderrors.New("std")),
	)
	got := fmt.Sprintf("%+v", err)
	t.Logf("\n%s", got)

	// Join, New(first), Errorf(second), New(x) の4箇所。標準のエラーには位置がない。
	if n := strings.Count(got, "errors_test.go:"); n != 4 {
		t.Errorf("フレーム数 = %d, want 4", n)
	}
}
