// Package errors は標準ライブラリの errors パッケージと同じ API を提供しつつ、
// エラーの生成・ラップ箇所(ファイル・行・関数)を記録するチーム標準のエラーパッケージ。
//
// 標準の errors を置き換えて使う:
//
//	import "github.com/Moiterika/errors"
//
//	var ErrNotFound = errors.New("not found")
//
//	func find(id string) error {
//		if err := query(id); err != nil {
//			return errors.Errorf("find %s: %w", id, err)
//		}
//		...
//	}
//
//	if errors.Is(err, ErrNotFound) { ... }
//
// Error() と %v / %s の出力は標準の errors / fmt.Errorf と同一。
// %+v で書式化すると、ラップの連鎖をたどって各エラーの生成箇所を出力する。
//
// 記録するのは New / Errorf / Join を呼んだ箇所の1フレームだけで、
// 実行時のスタック全体ではない。ラップするたびに1フレームずつ積み上がる。
//
// 内部では golang.org/x/xerrors のうち非推奨でない機能
// (Caller, Frame, Formatter, Printer, FormatError) だけを使い、
// Is / As / Unwrap は標準の errors に委譲する。
package errors

import (
	stderrors "errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/xerrors"
)

// ErrUnsupported は標準の [errors.ErrUnsupported] と同一の値。
var ErrUnsupported = stderrors.ErrUnsupported

// New は text をメッセージとするエラーを返す。呼び出し箇所を記録する。
func New(text string) error {
	return &fundamental{msg: text, frame: xerrors.Caller(1)}
}

// Errorf は [fmt.Errorf] と同じ規則でエラーを生成し、呼び出し箇所を記録する。
// %w は標準と同様に複数指定でき、ラップされたエラーは Is / As でたどれる。
func Errorf(format string, a ...any) error {
	return withFrame(fmt.Errorf(format, a...), xerrors.Caller(1))
}

// Join は [errors.Join] と同じ規則でエラーを結合し、呼び出し箇所を記録する。
// errs がすべて nil の場合は nil を返す。
func Join(errs ...error) error {
	var nonNil []error
	for _, err := range errs {
		if err != nil {
			nonNil = append(nonNil, err)
		}
	}
	if len(nonNil) == 0 {
		return nil
	}
	return &joinError{errs: nonNil, frame: xerrors.Caller(1)}
}

// Is は [errors.Is] と同一。
func Is(err, target error) bool { return stderrors.Is(err, target) }

// As は [errors.As] と同一。
func As(err error, target any) bool { return stderrors.As(err, target) }

// AsType は [errors.AsType] と同一。
func AsType[E error](err error) (E, bool) { return stderrors.AsType[E](err) }

// Unwrap は [errors.Unwrap] と同一。
func Unwrap(err error) error { return stderrors.Unwrap(err) }

// withFrame は fmt.Errorf の結果を、同じメッセージ・同じラップ構造のまま
// frame 付きの型に詰め替える。
func withFrame(err error, frame xerrors.Frame) error {
	switch u := err.(type) {
	case interface{ Unwrap() []error }:
		return &wrapErrors{msg: err.Error(), errs: u.Unwrap(), frame: frame}
	case interface{ Unwrap() error }:
		return &wrapError{msg: err.Error(), err: u.Unwrap(), frame: frame}
	default:
		return &fundamental{msg: err.Error(), frame: frame}
	}
}

// format は %+v のときだけ xerrors の詳細表示を使い、それ以外は
// 標準のエラーと同じく Error() の文字列を書式化する。
func format(f xerrors.Formatter, s fmt.State, verb rune) {
	if verb == 'v' && s.Flag('+') {
		xerrors.FormatError(f, s, verb)
		return
	}
	// Errorf("...: %w", err) などで最も多い、フラグなしの %v / %s は直接書き込む。
	if (verb == 'v' || verb == 's') && !hasFlagOrWidth(s) {
		io.WriteString(s, f.Error())
		return
	}
	fmt.Fprintf(s, fmt.FormatString(s, verb), f.Error())
}

func hasFlagOrWidth(s fmt.State) bool {
	if _, ok := s.Width(); ok {
		return true
	}
	if _, ok := s.Precision(); ok {
		return true
	}
	return s.Flag('-') || s.Flag('+') || s.Flag('#') || s.Flag(' ') || s.Flag('0')
}

// fundamental はラップ先を持たないエラー。
type fundamental struct {
	msg   string
	frame xerrors.Frame
}

func (e *fundamental) Error() string              { return e.msg }
func (e *fundamental) Format(s fmt.State, v rune) { format(e, s, v) }

func (e *fundamental) FormatError(p xerrors.Printer) error {
	p.Print(e.msg)
	e.frame.Format(p)
	return nil
}

// wrapError は %w を1つ含む Errorf の結果。
type wrapError struct {
	msg   string
	err   error
	frame xerrors.Frame
}

func (e *wrapError) Error() string              { return e.msg }
func (e *wrapError) Format(s fmt.State, v rune) { format(e, s, v) }
func (e *wrapError) Unwrap() error              { return e.err }

func (e *wrapError) FormatError(p xerrors.Printer) error {
	// "msg: %w" 形式なら、続けて表示されるラップ先のメッセージを重複させない。
	msg := e.msg
	if e.err != nil {
		msg = strings.TrimSuffix(msg, ": "+e.err.Error())
	}
	p.Print(msg)
	e.frame.Format(p)
	return e.err
}

// wrapErrors は %w を複数含む Errorf の結果。
type wrapErrors struct {
	msg   string
	errs  []error
	frame xerrors.Frame
}

func (e *wrapErrors) Error() string              { return e.msg }
func (e *wrapErrors) Format(s fmt.State, v rune) { format(e, s, v) }
func (e *wrapErrors) Unwrap() []error            { return e.errs }

func (e *wrapErrors) FormatError(p xerrors.Printer) error {
	p.Print(e.msg)
	e.frame.Format(p)
	printChildren(p, e.errs)
	return nil
}

// joinError は Join の結果。
type joinError struct {
	errs  []error
	frame xerrors.Frame
}

func (e *joinError) Error() string {
	// errors.Join と同じく、各エラーのメッセージを改行で連結する。
	msgs := make([]string, len(e.errs))
	for i, err := range e.errs {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "\n")
}

func (e *joinError) Format(s fmt.State, v rune) { format(e, s, v) }
func (e *joinError) Unwrap() []error            { return e.errs }

func (e *joinError) FormatError(p xerrors.Printer) error {
	p.Print("joined errors")
	e.frame.Format(p)
	printChildren(p, e.errs)
	return nil
}

// printChildren は複数のラップ先それぞれの詳細を出力する。
// xerrors.Formatter は次のエラーを1つしか返せないため、ここで展開する。
func printChildren(p xerrors.Printer, errs []error) {
	if !p.Detail() {
		return
	}
	for i, err := range errs {
		p.Printf("[%d] %+v\n", i, err)
	}
}
