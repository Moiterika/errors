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
// 標準ライブラリだけに依存する。発生箇所は runtime.Callers で記録し、
// Is / As / Unwrap は標準の errors に委譲する。
package errors

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"io"
	"runtime"
	"strings"
)

// ErrUnsupported は標準の [errors.ErrUnsupported] と同一の値。
var ErrUnsupported = stderrors.ErrUnsupported

// New は text をメッセージとするエラーを返す。呼び出し箇所を記録する。
func New(text string) error {
	return &fundamental{msg: text, frame: caller()}
}

// Errorf は [fmt.Errorf] と同じ規則でエラーを生成し、呼び出し箇所を記録する。
// %w は標準と同様に複数指定でき、ラップされたエラーは Is / As でたどれる。
func Errorf(format string, a ...any) error {
	return withFrame(fmt.Errorf(format, a...), caller())
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
	return &joinError{errs: nonNil, frame: caller()}
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
func withFrame(err error, frame frame) error {
	switch u := err.(type) {
	case interface{ Unwrap() []error }:
		return &wrapErrors{msg: err.Error(), errs: u.Unwrap(), frame: frame}
	case interface{ Unwrap() error }:
		return &wrapError{msg: err.Error(), err: u.Unwrap(), frame: frame}
	default:
		return &fundamental{msg: err.Error(), frame: frame}
	}
}

// frame は New / Errorf / Join を呼んだ箇所。
type frame struct {
	pc [1]uintptr
}

// caller は New / Errorf / Join の呼び出し元を記録する。
// New / Errorf / Join から直接呼ぶこと。
func caller() frame {
	var f frame
	runtime.Callers(3, f.pc[:]) // runtime.Callers, caller, New / Errorf / Join を飛ばす
	return f
}

// format は関数名とファイル名・行番号を詳細として出力する。
func (f frame) format(p *printer) {
	p.detail()
	fr, _ := runtime.CallersFrames(f.pc[:]).Next()
	if fr.Function != "" {
		p.printf("%s\n    ", fr.Function)
	}
	if fr.File != "" {
		p.printf("%s:%d\n", fr.File, fr.Line)
	}
}

// formatter はこのパッケージのエラー型。%+v のときに自分のメッセージと発生箇所を
// p に出力し、続けて出力するラップ先を返す。
type formatter interface {
	error
	formatError(p *printer) (next error)
}

// format は %+v のときだけ発生箇所付きの詳細を出力し、それ以外は
// 標準のエラーと同じく Error() の文字列を書式化する。
func format(f formatter, s fmt.State, verb rune) {
	if verb == 'v' && s.Flag('+') {
		formatDetail(f, s)
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

// formatDetail はラップの連鎖をたどり、各エラーのメッセージと発生箇所を出力する。
// 出力の形式は xerrors.FormatError の %+v と同じ。
func formatDetail(f formatter, s fmt.State) {
	p := &printer{State: s}
	var err error = f
loop:
	for {
		switch v := err.(type) {
		case formatter:
			err = v.formatError(p)
		case fmt.Formatter:
			// 他のライブラリのエラーは、そのエラー自身の %+v に任せる。
			v.Format(p, 'v')
			break loop
		default:
			p.buf.WriteString(v.Error())
			break loop
		}
		if err == nil {
			break
		}
		if p.needColon {
			p.buf.WriteByte(':')
			p.needColon = false
		}
		p.buf.WriteString("\n  - ")
		p.inDetail = false
		p.needNewline = false
	}

	_, okW := s.Width()
	_, okP := s.Precision()
	if okW || okP {
		fmt.Fprintf(s, fmt.FormatString(s, 'v'), p.buf.String())
		return
	}
	s.Write(p.buf.Bytes())
}

// printer は %+v の出力をためる。xerrors の Printer と同じ規則で、
// メッセージの後に ":" を付け、詳細(発生箇所)の各行を字下げする。
// 他のライブラリのエラーの Format に渡すため、fmt.State も満たす。
type printer struct {
	fmt.State
	buf bytes.Buffer

	inDetail    bool // detail を呼んだ後で、詳細を出力している
	needColon   bool // メッセージの後に ":" がまだ付いていない
	needNewline bool // 次に書く前に改行と字下げが要る
}

const detailSep = "\n    "

func (p *printer) Write(b []byte) (int, error) {
	n := len(b)
	if n == 0 {
		return 0, nil
	}
	if p.inDetail && p.needColon {
		p.needNewline = true
		if b[0] == '\n' {
			b = b[1:]
		}
	}
	k := 0
	for i, c := range b {
		if p.needNewline {
			if p.inDetail && p.needColon {
				p.buf.WriteByte(':')
				p.needColon = false
			}
			p.buf.WriteString(detailSep)
			p.needNewline = false
		}
		if c == '\n' {
			p.buf.Write(b[k:i])
			k = i + 1
			p.needNewline = true
		}
	}
	p.buf.Write(b[k:])
	if !p.inDetail {
		p.needColon = true
	}
	return n, nil
}

func (p *printer) print(s string) { io.WriteString(p, s) }

func (p *printer) printf(format string, a ...any) { fmt.Fprintf(p, format, a...) }

// detail は、以降の出力を詳細として扱うよう切り替える。
func (p *printer) detail() { p.inDetail = true }

// fundamental はラップ先を持たないエラー。
type fundamental struct {
	msg   string
	frame frame
}

func (e *fundamental) Error() string              { return e.msg }
func (e *fundamental) Format(s fmt.State, v rune) { format(e, s, v) }

func (e *fundamental) formatError(p *printer) error {
	p.print(e.msg)
	e.frame.format(p)
	return nil
}

// wrapError は %w を1つ含む Errorf の結果。
type wrapError struct {
	msg   string
	err   error
	frame frame
}

func (e *wrapError) Error() string              { return e.msg }
func (e *wrapError) Format(s fmt.State, v rune) { format(e, s, v) }
func (e *wrapError) Unwrap() error              { return e.err }

func (e *wrapError) formatError(p *printer) error {
	// "msg: %w" 形式なら、続けて表示されるラップ先のメッセージを重複させない。
	msg := e.msg
	if e.err != nil {
		msg = strings.TrimSuffix(msg, ": "+e.err.Error())
	}
	p.print(msg)
	e.frame.format(p)
	return e.err
}

// wrapErrors は %w を複数含む Errorf の結果。
type wrapErrors struct {
	msg   string
	errs  []error
	frame frame
}

func (e *wrapErrors) Error() string              { return e.msg }
func (e *wrapErrors) Format(s fmt.State, v rune) { format(e, s, v) }
func (e *wrapErrors) Unwrap() []error            { return e.errs }

func (e *wrapErrors) formatError(p *printer) error {
	p.print(e.msg)
	e.frame.format(p)
	printChildren(p, e.errs)
	return nil
}

// joinError は Join の結果。
type joinError struct {
	errs  []error
	frame frame
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

func (e *joinError) formatError(p *printer) error {
	p.print("joined errors")
	e.frame.format(p)
	printChildren(p, e.errs)
	return nil
}

// printChildren は複数のラップ先それぞれの詳細を出力する。
// formatError は次のエラーを1つしか返せないため、ここで展開する。
func printChildren(p *printer, errs []error) {
	p.detail()
	for i, err := range errs {
		p.printf("[%d] %+v\n", i, err)
	}
}
