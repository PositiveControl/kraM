package main

import "fmt"

type TokKind int

const (
	EOF TokKind = iota
	ILLEGAL
	NUMBER
	PLUS
	MINUS
	STAR
	SLASH
	LPAREN
	RPAREN
)

type Token struct {
	Kind TokKind
	Lit  string // source text (number digits, operator char)
	Pos  int    // byte offset, for error messages
}

func (t Token) String() string {
	return fmt.Sprintf("%s(%q)", kindName(t.Kind), t.Lit)
}

func kindName(k TokKind) string {
	switch k {
	case EOF:
		return "EOF"
	case NUMBER:
		return "NUMBER"
	case PLUS:
		return "PLUS"
	case MINUS:
		return "MINUS"
	case STAR:
		return "STAR"
	case SLASH:
		return "SLASH"
	case LPAREN:
		return "LPAREN"
	case RPAREN:
		return "RPAREN"
	default:
		return "ILLEGAL"
	}
}

// Lex turns source into a flat token slice ending in EOF.
func Lex(src string) []Token {
	var toks []Token
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '+':
			toks = append(toks, Token{PLUS, "+", i})
			i++
		case c == '-':
			toks = append(toks, Token{MINUS, "-", i})
			i++
		case c == '*':
			toks = append(toks, Token{STAR, "*", i})
			i++
		case c == '/':
			toks = append(toks, Token{SLASH, "/", i})
			i++
		case c == '(':
			toks = append(toks, Token{LPAREN, "(", i})
			i++
		case c == ')':
			toks = append(toks, Token{RPAREN, ")", i})
			i++
		case isDigit(c) || c == '.':
			start := i
			for i < len(src) && (isDigit(src[i]) || src[i] == '.') {
				i++
			}
			toks = append(toks, Token{NUMBER, src[start:i], start})
		default:
			// consume one rune so the lexer always advances
			toks = append(toks, Token{ILLEGAL, string(c), i})
			i++
		}
	}
	toks = append(toks, Token{EOF, "", i})
	return toks
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
