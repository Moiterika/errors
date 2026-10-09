package bench

import (
	stderrors "errors"
	"fmt"
	"testing"

	crdb "github.com/cockroachdb/errors"
	myerr "github.com/Moiterika/errors"
)

// 比較対象ごとの API。いずれも標準と同じ形の関数だけを使う。
type lib struct {
	name   string
	New    func(string) error
	Errorf func(string, ...any) error
	Is     func(error, error) bool
}

var libs = []lib{
	{"std", stderrors.New, fmt.Errorf, stderrors.Is},
	{"myerr", myerr.New, myerr.Errorf, myerr.Is},
	{"cockroachdb", crdb.New, crdb.Errorf, crdb.Is},
}

var (
	sinkErr  error
	sinkBool bool
	sinkStr  string
)

// atDepth は呼び出しを depth 段深くしてから f を実行する。
// スタック全体を取る実装は、呼び出しの深さでコストが変わるため。
//
//go:noinline
func atDepth(depth int, f func() error) error {
	if depth == 0 {
		return f()
	}
	return atDepth(depth-1, f)
}

// chain は sentinel を起点に、各段で Errorf("...: %w") するエラーを作る。
func chain(l lib, sentinel error, levels int) error {
	err := sentinel
	for i := range levels {
		err = l.Errorf("level %d: %w", i, err)
	}
	return err
}

func BenchmarkNew(b *testing.B) {
	for _, l := range libs {
		b.Run(l.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkErr = l.New("boom")
			}
		})
	}
}

func BenchmarkNewDeepStack(b *testing.B) {
	for _, l := range libs {
		b.Run(l.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkErr = atDepth(30, func() error { return l.New("boom") })
			}
		})
	}
}

func BenchmarkErrorfWrap(b *testing.B) {
	for _, l := range libs {
		base := l.New("base")
		b.Run(l.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkErr = l.Errorf("read %s: %w", "config.yaml", base)
			}
		})
	}
}

func BenchmarkWrapChain5(b *testing.B) {
	for _, l := range libs {
		sentinel := l.New("sentinel")
		b.Run(l.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkErr = chain(l, sentinel, 5)
			}
		})
	}
}

func BenchmarkIsChain5(b *testing.B) {
	for _, l := range libs {
		sentinel := l.New("sentinel")
		other := l.New("other")
		err := chain(l, sentinel, 5)
		b.Run(l.name+"/hit", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkBool = l.Is(err, sentinel)
			}
		})
		b.Run(l.name+"/miss", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkBool = l.Is(err, other)
			}
		})
	}
}

func BenchmarkErrorString(b *testing.B) {
	for _, l := range libs {
		err := chain(l, l.New("sentinel"), 5)
		b.Run(l.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkStr = err.Error()
			}
		})
	}
}

func BenchmarkFormatPlusV(b *testing.B) {
	for _, l := range libs {
		err := chain(l, l.New("sentinel"), 5)
		b.Run(l.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sinkStr = fmt.Sprintf("%+v", err)
			}
		})
	}
}

// 出力量の比較用。go test -run TestOutputSize -v で確認する。
func TestOutputSize(t *testing.T) {
	for _, l := range libs {
		err := chain(l, l.New("sentinel"), 5)
		t.Logf("%-12s %%+v = %6d bytes", l.name, len(fmt.Sprintf("%+v", err)))
	}
}
