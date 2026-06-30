package main

import (
	"fmt"
	"strconv"
)

// ---- AST ----

type Node interface{ node() }

type NumberLit struct{ Val float64 }
type Unary struct {
	Op    TokKind // MINUS
	Right Node
}
type Binary struct {
	Op          TokKind
	Left, Right Node
}

func (NumberLit) node() {}
func (Unary) node()     {}
func (Binary) node()    {}

// ---- Pratt parser ----

// Binding power per infix operator. Higher binds tighter.
// Add new operators here; the loop in parseExpr needs no changes.
var infixBP = map[TokKind]int{
	PLUS:  10,
	MINUS: 10,
	STAR:  20,
	SLASH: 20,
}

type Parser struct {
	toks []Token
	pos  int
	err  error
}

func Parse(src string) (Node, error) {
	p := &Parser{toks: Lex(src)}
	n := p.parseExpr(0)
	if p.err != nil {
		return nil, p.err
	}
	if p.cur().Kind != EOF {
		return nil, fmt.Errorf("unexpected %s", p.cur())
	}
	return n, nil
}

func (p *Parser) cur() Token { return p.toks[p.pos] }
func (p *Parser) advance() Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

// parseExpr is the Pratt loop: parse a prefix, then fold infix operators
// whose binding power exceeds minBP.
func (p *Parser) parseExpr(minBP int) Node {
	left := p.parsePrefix()
	for p.err == nil {
		bp, ok := infixBP[p.cur().Kind]
		if !ok || bp < minBP {
			break
		}
		op := p.advance().Kind
		// left-associative: right side parses with bp+1
		right := p.parseExpr(bp + 1)
		left = Binary{Op: op, Left: left, Right: right}
	}
	return left
}

func (p *Parser) parsePrefix() Node {
	t := p.advance()
	switch t.Kind {
	case NUMBER:
		v, err := strconv.ParseFloat(t.Lit, 64)
		if err != nil {
			p.fail("bad number %q", t.Lit)
			return nil
		}
		return NumberLit{Val: v}
	case MINUS:
		return Unary{Op: MINUS, Right: p.parseExpr(30)} // unary binds tighter than * /
	case LPAREN:
		inner := p.parseExpr(0)
		if p.cur().Kind != RPAREN {
			p.fail("expected ')', got %s", p.cur())
			return nil
		}
		p.advance()
		return inner
	default:
		p.fail("unexpected %s", t)
		return nil
	}
}

func (p *Parser) fail(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}
