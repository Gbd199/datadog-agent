package parser

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

//go:generate stringer -type=TokenKind
type TokenKind int

const (
	Undefined TokenKind = iota
	EOF
	Arrow
	DoubleArrow
	LeftParenthesis
	RightParenthesis
	LeftSquareBracket
	RightSquareBracket
	LeftCurlyBracket
	RightCurlyBracket
	CommercialAt
	Star
	Dot
	Comma
	Colon
	NumberLiteral
	StringLiteral
	Identifier
	TypeKeyword
	StructKeyword
	TrueKeyword
	FalseKeyword
	EmbedKeyword
	DocComment
)

type Token struct {
	Kind     TokenKind
	Content  string
	Position int
}

func (tok Token) String() string {
	return fmt.Sprintf("Token{%s, `%s`, #%d}", tok.Kind, tok.Content, tok.Position)
}

type literalTokenDefinition struct {
	kind    TokenKind
	literal string
}

type regexpTokenDefinition struct {
	kind         TokenKind
	reg          *regexp.Regexp
	contentIndex int
}

type Tokenizer struct {
	literal  []literalTokenDefinition
	regexps  []regexpTokenDefinition
	keywords map[string]TokenKind

	index   int
	content string
}

func NewTokenizer(content string) *Tokenizer {
	identifierRegexp := regexp.MustCompile(`^[a-zA-Z_][0-9a-zA-Z_]*`)
	numberLiteral := regexp.MustCompile(`^[0-9]+`)
	stringLiteral := regexp.MustCompile(`^"([^"]*)"`)
	stringLiteral2 := regexp.MustCompile("^`([^`]*)`")
	docRegexp := regexp.MustCompile(`^//!\s*(.*)\n`)

	return &Tokenizer{
		literal: []literalTokenDefinition{
			{Arrow, "->"},
			{DoubleArrow, "=>"},
			{LeftParenthesis, "("},
			{RightParenthesis, ")"},
			{LeftSquareBracket, "["},
			{RightSquareBracket, "]"},
			{LeftCurlyBracket, "{"},
			{RightCurlyBracket, "}"},
			{CommercialAt, "@"},
			{Star, "*"},
			{Dot, "."},
			{Comma, ","},
			{Colon, ":"},
		},
		regexps: []regexpTokenDefinition{
			{Identifier, identifierRegexp, 0},
			{DocComment, docRegexp, 1},
			{NumberLiteral, numberLiteral, 0},
			{StringLiteral, stringLiteral, 1},
			{StringLiteral, stringLiteral2, 1},
		},
		keywords: map[string]TokenKind{
			"type":   TypeKeyword,
			"struct": StructKeyword,
			"true":   TrueKeyword,
			"false":  FalseKeyword,
			"embed":  EmbedKeyword,
		},

		index:   0,
		content: content,
	}
}

func (t *Tokenizer) eatWhitespaces() {
	for !t.atEOF() && unicode.IsSpace(rune(t.content[t.index])) {
		t.index++
	}
}

func (t *Tokenizer) eatComment() bool {
	// eat comment
	if strings.HasPrefix(t.front(), "//") && !strings.HasPrefix(t.front(), "//!") {
		for !t.atEOF() && t.content[t.index] != '\n' {
			t.index++
		}

		// eat final \n
		if !t.atEOF() && t.content[t.index] == '\n' {
			t.index++
		}

		return true
	}

	return false
}

func (t *Tokenizer) atEOF() bool {
	return t.index >= len(t.content)
}

func (t *Tokenizer) front() string {
	return t.content[t.index:]
}

func (t *Tokenizer) NextToken() (Token, error) {
	t.eatWhitespaces()
	for t.eatComment() {
		t.eatWhitespaces()
	}

	if t.atEOF() {
		return Token{
			Kind:     EOF,
			Position: -1,
		}, nil
	}

	bestLen := -1
	tokenKind := Undefined
	content := ""
	for _, lit := range t.literal {
		litLen := len(lit.literal)
		if strings.HasPrefix(t.front(), lit.literal) && litLen > bestLen {
			bestLen = litLen
			tokenKind = lit.kind
			content = ""
		}
	}

	for _, reg := range t.regexps {
		locs := reg.reg.FindStringSubmatchIndex(t.front())

		if locs != nil {
			if locs[0] != 0 {
				panic("regexp match not at start")
			}

			if locs[1] > bestLen {
				bestLen = locs[1]
				tokenKind = reg.kind
				content = t.front()[locs[reg.contentIndex*2]:locs[reg.contentIndex*2+1]]
			}
		}
	}

	if bestLen < 0 {
		err := fmt.Errorf("unrecognized char `%c`", t.front()[0])
		t.index++
		return Token{}, err
	}

	if tokenKind == Identifier {
		if newTokenKind, ok := t.keywords[content]; ok {
			tokenKind = newTokenKind
		}
	}

	tok := Token{
		Kind:     tokenKind,
		Content:  content,
		Position: t.index,
	}
	t.index += bestLen
	return tok, nil
}
