package bench

import (
	"runtime"
	"testing"

	"golang.org/x/xerrors"
)

var (
	sinkFrame xerrors.Frame
	sinkPC    uintptr
	sinkPCs   [3]uintptr
)

// 呼び出し箇所の取り方ごとのコスト。
func BenchmarkCaller(b *testing.B) {
	b.Run("xerrors.Caller(3PCs)", func(b *testing.B) {
		for b.Loop() {
			sinkFrame = xerrors.Caller(1)
		}
	})
	b.Run("runtime.Callers(3PCs)", func(b *testing.B) {
		for b.Loop() {
			runtime.Callers(2, sinkPCs[:])
		}
	})
	b.Run("runtime.Callers(1PC)", func(b *testing.B) {
		for b.Loop() {
			runtime.Callers(2, sinkPCs[:1])
		}
	})
	b.Run("runtime.Caller", func(b *testing.B) {
		for b.Loop() {
			sinkPC, _, _, _ = runtime.Caller(1)
		}
	})
}
