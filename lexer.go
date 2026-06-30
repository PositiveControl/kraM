package main

import (
	"fmt"
	"strings"
)

type TokKind int

const (
	EOF TokKind = iota
	ILLEGAL
	NUMBER
	STRING
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
	IF
	ELSE
	WHILE
	LBRACE  // {
	RBRACE  // }
	SEMI    // ;
	PLUSEQ  // +=  reversible update
	MINUSEQ // -=  reversible update
	SWAP    // <=> reversible swap
	ASSERT  // assert (reversible runtime check)
	REVERSE // reverse { ... } (run a block's structural inverse)
	FROM    // from (reversible loop entry)
	LOOP    // loop (reversible loop step)
	UNTIL   // until (reversible loop exit)
	CARETEQ // ^= reversible XOR update (self-inverse, exact)
	PROC    // proc (define a procedure)
	CALL    // call (run a procedure forward)
	UNCALL  // uncall (run a procedure backward)
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
	case STRING:
		return "STRING"
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
	case IF:
		return "IF"
	case ELSE:
		return "ELSE"
	case WHILE:
		return "WHILE"
	case LBRACE:
		return "LBRACE"
	case RBRACE:
		return "RBRACE"
	case SEMI:
		return "SEMI"
	case PLUSEQ:
		return "PLUSEQ"
	case MINUSEQ:
		return "MINUSEQ"
	case SWAP:
		return "SWAP"
	case ASSERT:
		return "ASSERT"
	case REVERSE:
		return "REVERSE"
	case FROM:
		return "FROM"
	case LOOP:
		return "LOOP"
	case UNTIL:
		return "UNTIL"
	case CARETEQ:
		return "CARETEQ"
	case PROC:
		return "PROC"
	case CALL:
		return "CALL"
	case UNCALL:
		return "UNCALL"
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
		case c == '#': // line comment — skip to end of line
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '+':
			if peek(src, i+1) == '=' {
				toks = append(toks, Token{PLUSEQ, "+=", i})
				i += 2
			} else {
				toks = append(toks, Token{PLUS, "+", i})
				i++
			}
		case c == '-':
			if peek(src, i+1) == '=' {
				toks = append(toks, Token{MINUSEQ, "-=", i})
				i += 2
			} else {
				toks = append(toks, Token{MINUS, "-", i})
				i++
			}
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
		case c == '{':
			toks = append(toks, Token{LBRACE, "{", i})
			i++
		case c == '}':
			toks = append(toks, Token{RBRACE, "}", i})
			i++
		case c == ';':
			toks = append(toks, Token{SEMI, ";", i})
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
			if peek(src, i+1) == '=' && peek(src, i+2) == '>' {
				toks = append(toks, Token{SWAP, "<=>", i})
				i += 3
			} else if peek(src, i+1) == '=' {
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
		case c == '^':
			if peek(src, i+1) == '=' {
				toks = append(toks, Token{CARETEQ, "^=", i})
				i += 2
			} else {
				toks = append(toks, Token{ILLEGAL, "^", i})
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
		case c == '"':
			start := i
			i++ // skip opening quote
			var sb strings.Builder
			terminated := false
			for i < len(src) {
				ch := src[i]
				if ch == '\\' && i+1 < len(src) {
					switch src[i+1] {
					case 'n':
						sb.WriteByte('\n')
					case 't':
						sb.WriteByte('\t')
					case '"':
						sb.WriteByte('"')
					case '\\':
						sb.WriteByte('\\')
					default:
						sb.WriteByte(src[i+1])
					}
					i += 2
					continue
				}
				if ch == '"' {
					terminated = true
					i++
					break
				}
				sb.WriteByte(ch)
				i++
			}
			if terminated {
				toks = append(toks, Token{STRING, sb.String(), start})
			} else {
				toks = append(toks, Token{ILLEGAL, src[start:i], start})
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
			case "if":
				toks = append(toks, Token{IF, word, start})
			case "else":
				toks = append(toks, Token{ELSE, word, start})
			case "while":
				toks = append(toks, Token{WHILE, word, start})
			case "assert":
				toks = append(toks, Token{ASSERT, word, start})
			case "reverse":
				toks = append(toks, Token{REVERSE, word, start})
			case "from":
				toks = append(toks, Token{FROM, word, start})
			case "loop":
				toks = append(toks, Token{LOOP, word, start})
			case "until":
				toks = append(toks, Token{UNTIL, word, start})
			case "proc":
				toks = append(toks, Token{PROC, word, start})
			case "call":
				toks = append(toks, Token{CALL, word, start})
			case "uncall":
				toks = append(toks, Token{UNCALL, word, start})
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
