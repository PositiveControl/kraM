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
	IDENT
	ASSIGN
	TRUE
	FALSE
	LT // <
	GT // >
	LE // <=
	GE // >=
	EQ // ==
	NE // !=
	PRINT
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
	case IDENT:
		return "IDENT"
	case ASSIGN:
		return "ASSIGN"
	case TRUE:
		return "TRUE"
	case FALSE:
		return "FALSE"
	case LT:
		return "LT"
	case GT:
		return "GT"
	case LE:
		return "LE"
	case GE:
		return "GE"
	case EQ:
		return "EQ"
	case NE:
		return "NE"
	case PRINT:
		return "PRINT"
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
		case c == '=':
			if peek(src, i+1) == '=' {
				toks = append(toks, Token{EQ, "==", i})
				i += 2
			} else {
				toks = append(toks, Token{ASSIGN, "=", i})
				i++
			}
		case c == '<':
			if peek(src, i+1) == '=' {
				toks = append(toks, Token{LE, "<=", i})
				i += 2
			} else {
				toks = append(toks, Token{LT, "<", i})
				i++
			}
		case c == '>':
			if peek(src, i+1) == '=' {
				toks = append(toks, Token{GE, ">=", i})
				i += 2
			} else {
				toks = append(toks, Token{GT, ">", i})
				i++
			}
		case c == '!':
			if peek(src, i+1) == '=' {
				toks = append(toks, Token{NE, "!=", i})
				i += 2
			} else {
				toks = append(toks, Token{ILLEGAL, "!", i})
				i++
			}
		case isAlpha(c):
			start := i
			for i < len(src) && (isAlpha(src[i]) || isDigit(src[i])) {
				i++
			}
			word := src[start:i]
			switch word {
			case "true":
				toks = append(toks, Token{TRUE, word, start})
			case "false":
				toks = append(toks, Token{FALSE, word, start})
			case "print":
				toks = append(toks, Token{PRINT, word, start})
			default:
				toks = append(toks, Token{IDENT, word, start})
			}
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

func peek(src string, i int) byte {
	if i < len(src) {
		return src[i]
	}
	return 0
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
