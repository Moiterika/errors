# errors

チーム標準のエラーパッケージです。標準ライブラリの `errors` と同じ API を持ち、エラーを生成した箇所とラップした箇所を記録します。

- `Error()` / `%v` / `%s` の出力は標準の `errors` / `fmt.Errorf` と同一
- `Is` / `As` / `AsType` / `Unwrap` は標準の `errors` の関数をそのまま公開
- `%+v` で書式化すると、ラップの連鎖をたどって各エラーの発生箇所を出力
- 依存は `golang.org/x/xerrors` のみ。xerrors の非推奨関数は使わず、公開もしない

## 使い方

標準の `errors` の import をこのパッケージに差し替え、`fmt.Errorf` を `errors.Errorf` に置き換えます。
`AsType` を使うため、Go 1.26 以上が必要です。

```go
import "github.com/Moiterika/errors"

var ErrNotFound = errors.New("not found")

func LoadUser(id string) (*User, error) {
	u, err := repo.Find(id)
	if err != nil {
		return nil, errors.Errorf("load user %s: %w", id, err)
	}
	return u, nil
}

func handler() {
	_, err := LoadUser("42")
	if errors.Is(err, ErrNotFound) {
		// ...
	}
	if e, ok := errors.AsType[*ValidationError](err); ok {
		// ...
	}
	log.Printf("%+v", err) // 発生箇所付きで出力
}
```

### 出力

`%v` の出力は標準と同じです。

```
load user 42: find: not found
```

`%+v` では、各エラーのメッセージの下に発生箇所が付きます。

```
load user 42:
    example.com/app/user.LoadUser
        /src/app/user/user.go:24
  - find:
    example.com/app/repo.(*Repo).Find
        /src/app/repo/repo.go:51
  - not found:
    example.com/app/repo.init
        /src/app/repo/errors.go:5
```

`Join` で束ねたエラーや、`%w` を複数使ってラップしたエラーは、枝ごとに `[0]`, `[1]`, ... と番号を付けて展開されます。

## API

| 関数 | 標準での対応 | 発生箇所の記録 |
|---|---|---|
| `New(text string) error` | `errors.New` | あり |
| `Errorf(format string, a ...any) error` | `fmt.Errorf` | あり |
| `Join(errs ...error) error` | `errors.Join` | あり |
| `Is(err, target error) bool` | `errors.Is` | — |
| `As(err error, target any) bool` | `errors.As` | — |
| `AsType[E error](err error) (E, bool)` | `errors.AsType` | — |
| `Unwrap(err error) error` | `errors.Unwrap` | — |
| `ErrUnsupported` | `errors.ErrUnsupported`（同一の値） | — |

`Errorf` の書式の規則は `fmt.Errorf` と同じです。`%w` は複数指定でき、`%[2]w` のような添字指定も使えます。

このパッケージのエラーと標準のエラーは混在できます。このパッケージのエラーは標準の `errors.Is` / `errors.As` でたどれ、標準のエラーはこのパッケージでラップできます。ただし、標準のエラーには発生箇所が記録されていないため、`%+v` ではそのエラーのメッセージだけが出ます。

## チームでの運用ルール

- エラーは `errors.New` で作り、`errors.Errorf("...: %w", err)` でラップする。`fmt.Errorf` は発生箇所を記録しないので使わない。`pkg/errors` も使わない。
- 外部ライブラリから返ったエラーは、受け取った箇所で `Errorf` でラップする。外部ライブラリのエラーには発生箇所が記録されていないので、ラップした箇所が `%+v` に残る唯一の手がかりになる。
- 番兵エラー（`var ErrX = errors.New(...)`）を `%+v` で出しても、定義した行は分からない（パッケージ初期化の `init` と出るだけ）。返すときに `Errorf("...: %w", ErrX)` で包めば、実際に返した箇所が残る。
- エラーの比較には `==` ではなく `errors.Is` / `errors.As` / `errors.AsType` を使う。ラップしたエラーは元のエラーと別の値なので、`==` では一致しない。
- ログには `%+v` で出す。利用者に見せるメッセージには `Error()` か `%v` を使う。

## 設計

### 発生箇所の記録方法

`New` / `Errorf` / `Join` を呼んだ箇所を、呼び出しごとに1フレームだけ記録します。実行時のスタック全体は取りません。ラップするたびに1フレームずつ積み上がるので、`%+v` には、エラーを生成した箇所と、ラップした各箇所が出ます。

スタック全体を取る方式（`pkg/errors`、`cockroachdb/errors`）と比べると、ラップしなかった途中の呼び出し元が残らない分、記録する情報は少なくなります。その代わり、生成が速く、`%+v` の出力も短くなります。

### xerrors の使い方

xerrors で非推奨になっているのは `Is` / `As` / `Unwrap` だけです。このパッケージは、非推奨でない次の機能だけを内部で使っています。

- `xerrors.Caller` / `xerrors.Frame`: 発生箇所の記録と表示
- `xerrors.Formatter` / `xerrors.Printer` / `xerrors.FormatError`: `%+v` の出力

`xerrors.New` と `xerrors.Errorf` は使っていません。`xerrors.Errorf` は Go 1.20 で入った `%w` の複数指定に対応しておらず、`%w` を複数渡すとラップせずに `%!w(...)` を含む文字列を返すためです。このパッケージの `Errorf` は、メッセージとラップ構造の生成を `fmt.Errorf` に任せ、その結果に発生箇所を付け足しています。

## パフォーマンス

このパッケージを、標準（`errors` / `fmt.Errorf`）および `cockroachdb/errors` と比べた結果です。パフォーマンス指標とした`cockroachdb/errors` の出典は [github.com/cockroachdb/errors](https://github.com/cockroachdb/errors)（v1.14.0、[Apache License 2.0](https://github.com/cockroachdb/errors/blob/v1.14.0/LICENSE)）です。Go 1.26.4、Intel Core i5-12400F で各6回計測した中央値を示します。

| 処理 | 標準 | このパッケージ | cockroachdb/errors |
|---|---|---|---|
| `New` | 17 ns / 1 alloc | 175 ns / 1 alloc | 569 ns / 7 allocs |
| `Errorf("...: %w")` | 155 ns / 3 allocs | 390 ns / 4 allocs | 1,876 ns / 19 allocs |
| 5段ラップしたエラーを作る | 0.9 µs | 2.2 µs | 23.4 µs |
| `Is`（5段・一致する） | 20 ns | 28 ns | 130 ns |
| `Is`（5段・一致しない） | 24 ns | 34 ns | 850 ns |
| `Error()`（5段） | 1.7 ns | 1.7 ns | 1,320 ns |
| `%+v`（5段） | 0.1 µs | 6.7 µs | 858 µs |
| `%+v` の出力サイズ（5段） | 53 B | 672 B | 30,381 B |

エラーの生成で標準より遅くなる分は、大部分が発生箇所の記録（`runtime.Callers`、約150 ns）です。

ベンチマークは `bench/` にあります。このパッケージの依存に `cockroachdb/errors` が入らないよう、別モジュールにしています。

```sh
cd bench
go test -run '^$' -bench . -benchmem -count 6 .
```

## 開発

```sh
go vet ./...
go test ./...
```
