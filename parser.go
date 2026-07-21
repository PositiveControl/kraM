package main

import (
	"fmt"
	"strconv"
)

// ---- AST ----

type Node interface{ node() }

type NumberLit struct{ Val float64 }
type BoolLit struct{ Val bool }
type StrLit struct{ Val string }
type Var struct{ Name string }
type Assign struct {
	Name  string
	Value Node
}
type Print struct{ Value Node }

// CompoundAssign is a reversible update: `name += value` or `name -= value`.
// Op is PLUS or MINUS.
type CompoundAssign struct {
	Name  string
	Op    TokKind
	Value Node
}

// Swap exchanges two lvalues: `a <=> b` or `a[i] <=> a[j]`. AI/BI are the index
// expressions when an operand is an array element (nil for a plain variable).
// Self-inverse, no information lost.
type Swap struct {
	A, B   string
	AI, BI Node
}

// Local introduces a scoped variable: `local x = e` (x must be fresh).
// Delocal removes it: `delocal x = e` (x must currently equal e). They are
// exact inverses, so a scoped temporary is reversible.
type Local struct {
	Name  string
	Value Node
}
type Delocal struct {
	Name  string
	Value Node
}

// Forget is the deliberate irreversible erasure of a variable: `forget x`. It
// is the one escape hatch from the Janus discipline — the only way to destroy
// information. Removes the binding (undoable for time travel, but not
// structurally invertible: reverse{}/uncall reject it, and it has no gate).
type Forget struct{ Name string }

// ArrayLit builds an array value: [e0, e1, ...].
type ArrayLit struct{ Elems []Node }

// Index reads an array element: Arr[Idx].
type Index struct{ Arr, Idx Node }

// IdxAssign is destructive element assignment: a[i] = value.
type IdxAssign struct {
	Name  string
	Idx   Node
	Value Node
}

// IdxUpdate is a reversible element update: a[i] += / -= / ^= value. Op is the
// operator token (PLUSEQ, MINUSEQ, or CARETEQ).
type IdxUpdate struct {
	Name  string
	Idx   Node
	Op    TokKind
	Value Node
}

// XorAssign is `name ^= value` — a self-inverse, exact reversible update on
// integers. Maps to a CNOT-style gate.
type XorAssign struct {
	Name  string
	Value Node
}
type Block struct{ Stmts []Node }
type If struct {
	Cond, Then Node
	Else       Node // nil when there is no else
	Exit       Node // optional 'assert' exit condition; makes the if reversible
}

// Assert is a reversible runtime check: the condition must hold. Self-inverse.
type Assert struct{ Cond Node }

// Reverse runs the structural inverse of its body — backward execution derived
// from program text, not from the undo log.
type Reverse struct{ Body Node }

// ReversibleLoop is the Janus loop: from ENTRY { Do } loop { Rest } until EXIT.
// ENTRY must hold on first entry and fail on every re-entry; the loop ends when
// EXIT holds. Those assertions make it invertible without a log.
type ReversibleLoop struct {
	Entry    Node
	Do, Rest Node
	Exit     Node
}
type While struct {
	Cond, Body Node
}

// ProcDef defines a procedure. Params are by-reference variable names.
type ProcDef struct {
	Name   string
	Params []string
	Body   Node
}

// Call runs a procedure forward; Uncall runs its structural inverse. Args are
// the caller's variable names bound by-reference to the parameters.
type Call struct {
	Name string
	Args []string
}
type Uncall struct {
	Name string
	Args []string
}
type Unary struct {
	Op    TokKind // MINUS
	Right Node
}
type Binary struct {
	Op          TokKind
	Left, Right Node
}

func (NumberLit) node()      {}
func (BoolLit) node()        {}
func (StrLit) node()         {}
func (Var) node()            {}
func (Assign) node()         {}
func (Print) node()          {}
func (CompoundAssign) node() {}
func (Swap) node()           {}
func (XorAssign) node()      {}
func (Block) node()          {}
func (If) node()             {}
func (While) node()          {}
func (ProcDef) node()        {}
func (Call) node()           {}
func (Uncall) node()         {}
func (Assert) node()         {}
func (Reverse) node()        {}
func (ReversibleLoop) node() {}
func (Unary) node()          {}
func (Binary) node()         {}
func (ArrayLit) node()       {}
func (Index) node()          {}
func (IdxAssign) node()      {}
func (IdxUpdate) node()      {}
func (Local) node()          {}
func (Delocal) node()        {}
func (Forget) node()         {}

// ---- Pratt parser ----

// Binding power per infix operator. Higher binds tighter.
// Add new operators here; the loop in parseExpr needs no changes.
var infixBP = map[TokKind]int{
	OR:    2,
	AND:   3,
	EQ:    5,
	NE:    5,
	LT:    7,
	GT:    7,
	LE:    7,
	GE:    7,
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
	n := p.parseProgram()
	if p.err != nil {
		return nil, p.err
	}
	if p.cur().Kind != EOF {
		return nil, fmt.Errorf("unexpected %s", p.cur())
	}
	return n, nil
}

// parseProgram parses a top-level ';'-separated statement list. A lone
// statement is returned bare; multiples wrap in a Block.
func (p *Parser) parseProgram() Node {
	stmts := p.parseStmtList(EOF)
	if len(stmts) == 1 {
		return stmts[0]
	}
	return Block{Stmts: stmts}
}

// parseStmtList reads statements separated by ';' up to `end` (RBRACE or EOF).
// Stray/trailing ';' are tolerated.
func (p *Parser) parseStmtList(end TokKind) []Node {
	var stmts []Node
	for p.err == nil && p.cur().Kind != end && p.cur().Kind != EOF {
		if p.cur().Kind == SEMI {
			p.advance()
			continue
		}
		stmts = append(stmts, p.parseStmt())
	}
	return stmts
}

// parseStmt: print / if / block / assignment / bare expression.
func (p *Parser) parseStmt() Node {
	switch p.cur().Kind {
	case PRINT:
		p.advance()
		return Print{Value: p.parseExpr(0)}
	case IF:
		return p.parseIf()
	case ASSERT:
		p.advance()
		return Assert{Cond: p.parseExpr(0)}
	case REVERSE:
		p.advance()
		return Reverse{Body: p.parseBlock()}
	case LOCAL:
		p.advance()
		name := p.declName()
		p.expect(ASSIGN, "'='")
		return Local{Name: name, Value: p.parseExpr(0)}
	case DELOCAL:
		p.advance()
		name := p.declName()
		p.expect(ASSIGN, "'='")
		return Delocal{Name: name, Value: p.parseExpr(0)}
	case FORGET:
		p.advance()
		return Forget{Name: p.declName()}
	case WITH:
		return p.parseWith()
	case PROC:
		p.advance()
		if p.cur().Kind != IDENT {
			p.fail("expected procedure name, got %s", p.cur())
			return nil
		}
		name := p.advance().Lit
		params := p.parseNameList()
		return ProcDef{Name: name, Params: params, Body: p.parseBlock()}
	case CALL:
		p.advance()
		if p.cur().Kind != IDENT {
			p.fail("expected procedure name after 'call', got %s", p.cur())
			return nil
		}
		name := p.advance().Lit
		return Call{Name: name, Args: p.parseNameList()}
	case UNCALL:
		p.advance()
		if p.cur().Kind != IDENT {
			p.fail("expected procedure name after 'uncall', got %s", p.cur())
			return nil
		}
		name := p.advance().Lit
		return Uncall{Name: name, Args: p.parseNameList()}
	case FROM:
		p.advance()
		entry := p.parseExpr(0)
		do := p.parseBlock()
		p.expect(LOOP, "'loop'")
		rest := p.parseBlock()
		p.expect(UNTIL, "'until'")
		exit := p.parseExpr(0)
		return ReversibleLoop{Entry: entry, Do: do, Rest: rest, Exit: exit}
	case WHILE:
		p.advance()
		cond := p.parseExpr(0)
		body := p.parseBlock()
		return While{Cond: cond, Body: body}
	case LBRACE:
		return p.parseBlock()
	}
	// Parse an expression; if an assignment operator follows, the expression was
	// a target (a variable or an indexed element). Otherwise it is a bare
	// expression statement.
	left := p.parseExpr(0)
	switch p.cur().Kind {
	case ASSIGN:
		p.advance()
		return p.makeAssign(left, p.parseExpr(0))
	case PLUSEQ, MINUSEQ, CARETEQ:
		op := p.advance().Kind
		return p.makeUpdate(left, op, p.parseExpr(0))
	case SWAP:
		p.advance()
		return p.makeSwap(left, p.parseExpr(0))
	}
	return left
}

// lvalue extracts (name, index) from an assignable expression. index is nil for
// a plain variable. Only single-level indexing (a[i]) is assignable.
func (p *Parser) lvalue(n Node) (string, Node, bool) {
	switch v := n.(type) {
	case Var:
		return v.Name, nil, true
	case Index:
		if base, ok := v.Arr.(Var); ok {
			return base.Name, v.Idx, true
		}
	}
	return "", nil, false
}

func (p *Parser) makeAssign(target, val Node) Node {
	name, idx, ok := p.lvalue(target)
	if !ok {
		p.fail("cannot assign to this expression")
		return nil
	}
	if idx == nil {
		return Assign{Name: name, Value: val}
	}
	return IdxAssign{Name: name, Idx: idx, Value: val}
}

func (p *Parser) makeUpdate(target Node, op TokKind, val Node) Node {
	name, idx, ok := p.lvalue(target)
	if !ok {
		p.fail("cannot update this expression")
		return nil
	}
	if idx != nil {
		return IdxUpdate{Name: name, Idx: idx, Op: op, Value: val}
	}
	if op == CARETEQ {
		return XorAssign{Name: name, Value: val}
	}
	arith := PLUS
	if op == MINUSEQ {
		arith = MINUS
	}
	return CompoundAssign{Name: name, Op: arith, Value: val}
}

func (p *Parser) makeSwap(a, b Node) Node {
	an, ai, aok := p.lvalue(a)
	bn, bi, bok := p.lvalue(b)
	if !aok || !bok {
		p.fail("'<=>' operands must be variables or array elements")
		return nil
	}
	return Swap{A: an, AI: ai, B: bn, BI: bi}
}

func (p *Parser) parseBlock() Node {
	if p.cur().Kind != LBRACE {
		p.fail("expected '{', got %s", p.cur())
		return nil
	}
	p.advance()
	stmts := p.parseStmtList(RBRACE)
	if p.cur().Kind != RBRACE {
		p.fail("expected '}', got %s", p.cur())
		return nil
	}
	p.advance()
	return Block{Stmts: stmts}
}

// parseWith: `with x = e { compute } do { body }` — Bennett's compute-copy-
// uncompute as syntax. Desugars at parse time to
//   local x = e; compute; body; inverse(compute); delocal x = e
// so eval, reverse{}, uncall, time travel, and circuit lowering all treat it
// as the sequence it means. Inverting compute here makes reversibility of the
// compute block a parse-time check.
func (p *Parser) parseWith() Node {
	p.advance() // 'with'
	name := p.declName()
	p.expect(ASSIGN, "'='")
	val := p.parseExpr(0)
	comp := p.parseBlock()
	p.expect(DO, "'do'")
	body := p.parseBlock()
	if p.err != nil {
		return nil
	}
	uncomp, err := invert(comp)
	if err != nil {
		p.fail("with: compute block is not reversible: %v", err)
		return nil
	}
	return Block{Stmts: []Node{
		Local{Name: name, Value: val},
		comp,
		body,
		uncomp,
		Delocal{Name: name, Value: val},
	}}
}

// parseIf: `if cond { ... }` with optional `else { ... }` or `else if ...`.
func (p *Parser) parseIf() Node {
	p.advance() // 'if'
	cond := p.parseExpr(0)
	then := p.parseBlock()
	var els Node
	if p.cur().Kind == ELSE {
		p.advance()
		if p.cur().Kind == IF {
			els = p.parseIf() // else-if chain
		} else {
			els = p.parseBlock()
		}
	}
	// Optional exit assertion makes the if reversible: 'if c {..} else {..} assert e'.
	var exit Node
	if p.cur().Kind == ASSERT {
		p.advance()
		exit = p.parseExpr(0)
	}
	return If{Cond: cond, Then: then, Else: els, Exit: exit}
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
	for p.cur().Kind == LBRACK { // postfix indexing: a[i], a[i][j]
		p.advance()
		idx := p.parseExpr(0)
		p.expect(RBRACK, "']'")
		left = Index{Arr: left, Idx: idx}
	}
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
	case STRING:
		return StrLit{Val: t.Lit}
	case IDENT:
		return Var{Name: t.Lit}
	case TRUE:
		return BoolLit{Val: true}
	case FALSE:
		return BoolLit{Val: false}
	case MINUS:
		return Unary{Op: MINUS, Right: p.parseExpr(30)} // unary binds tighter than * /
	case NOT:
		return Unary{Op: NOT, Right: p.parseExpr(30)} // boolean negation
	case LPAREN:
		inner := p.parseExpr(0)
		if p.cur().Kind != RPAREN {
			p.fail("expected ')', got %s", p.cur())
			return nil
		}
		p.advance()
		return inner
	case LBRACK: // array literal [e, e, ...]
		var elems []Node
		for p.cur().Kind != RBRACK && p.cur().Kind != EOF && p.err == nil {
			elems = append(elems, p.parseExpr(0))
			if p.cur().Kind == COMMA {
				p.advance()
			}
		}
		p.expect(RBRACK, "']'")
		return ArrayLit{Elems: elems}
	default:
		p.fail("unexpected %s", t)
		return nil
	}
}

// declName reads a variable name in a declaration position.
func (p *Parser) declName() string {
	if p.cur().Kind != IDENT {
		p.fail("expected a variable name, got %s", p.cur())
		return ""
	}
	return p.advance().Lit
}

// parseNameList parses an optional `(a, b, c)` list of identifiers. Returns nil
// (not an empty slice) when there are no parentheses, so paramless procs work.
func (p *Parser) parseNameList() []string {
	if p.cur().Kind != LPAREN {
		return nil
	}
	p.advance()
	var names []string
	for p.cur().Kind != RPAREN && p.cur().Kind != EOF {
		if p.cur().Kind != IDENT {
			p.fail("expected a variable name, got %s", p.cur())
			return names
		}
		names = append(names, p.advance().Lit)
		if p.cur().Kind == COMMA {
			p.advance()
		}
	}
	p.expect(RPAREN, "')'")
	return names
}

// expect consumes a token of the given kind or records an error.
func (p *Parser) expect(k TokKind, what string) {
	if p.cur().Kind != k {
		p.fail("expected %s, got %s", what, p.cur())
		return
	}
	p.advance()
}

func (p *Parser) fail(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}
