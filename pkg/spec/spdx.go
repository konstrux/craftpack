// SPDX-FileCopyrightText: 2026 Marcin Kaim
// SPDX-License-Identifier: GPL-3.0-only

package spec

import (
	"strings"
	"unicode"
)

// spdxLicenses contains official SPDX 3.x / 2.x standard license identifiers.
var spdxLicenses = map[string]struct{}{
	"0BSD":                {},
	"AAL":                 {},
	"AFL-1.1":             {},
	"AFL-1.2":             {},
	"AFL-2.0":             {},
	"AFL-2.1":             {},
	"AFL-3.0":             {},
	"AGPL-1.0-only":       {},
	"AGPL-1.0-or-later":   {},
	"AGPL-3.0-only":       {},
	"AGPL-3.0-or-later":   {},
	"Apache-1.0":          {},
	"Apache-1.1":          {},
	"Apache-2.0":          {},
	"APL-1.0":             {},
	"Artistic-1.0":        {},
	"Artistic-1.0-cl8":    {},
	"Artistic-1.0-Perl":   {},
	"Artistic-2.0":        {},
	"BSD-1-Clause":        {},
	"BSD-2-Clause":        {},
	"BSD-2-Clause-Patent": {},
	"BSD-3-Clause":        {},
	"BSD-3-Clause-Clear":  {},
	"BSD-4-Clause":        {},
	"BSL-1.0":             {},
	"BUSL-1.1":            {},
	"CC-BY-1.0":           {},
	"CC-BY-2.0":           {},
	"CC-BY-2.5":           {},
	"CC-BY-3.0":           {},
	"CC-BY-4.0":           {},
	"CC-BY-NC-1.0":        {},
	"CC-BY-NC-2.0":        {},
	"CC-BY-NC-2.5":        {},
	"CC-BY-NC-3.0":        {},
	"CC-BY-NC-4.0":        {},
	"CC-BY-NC-ND-1.0":     {},
	"CC-BY-NC-ND-2.0":     {},
	"CC-BY-NC-ND-2.5":     {},
	"CC-BY-NC-ND-3.0":     {},
	"CC-BY-NC-ND-4.0":     {},
	"CC-BY-NC-SA-1.0":     {},
	"CC-BY-NC-SA-2.0":     {},
	"CC-BY-NC-SA-2.5":     {},
	"CC-BY-NC-SA-3.0":     {},
	"CC-BY-NC-SA-4.0":     {},
	"CC-BY-ND-1.0":        {},
	"CC-BY-ND-2.0":        {},
	"CC-BY-ND-2.5":        {},
	"CC-BY-ND-3.0":        {},
	"CC-BY-ND-4.0":        {},
	"CC-BY-SA-1.0":        {},
	"CC-BY-SA-2.0":        {},
	"CC-BY-SA-2.5":        {},
	"CC-BY-SA-3.0":        {},
	"CC-BY-SA-4.0":        {},
	"CC0-1.0":             {},
	"CDDL-1.0":            {},
	"CDDL-1.1":            {},
	"CECILL-1.0":          {},
	"CECILL-2.0":          {},
	"CECILL-2.1":          {},
	"CECILL-B":            {},
	"CECILL-C":            {},
	"CPL-1.0":             {},
	"ECL-1.0":             {},
	"ECL-2.0":             {},
	"EFL-1.0":             {},
	"EFL-2.0":             {},
	"EPL-1.0":             {},
	"EPL-2.0":             {},
	"EUPL-1.0":            {},
	"EUPL-1.1":            {},
	"EUPL-1.2":            {},
	"GPL-1.0-only":        {},
	"GPL-1.0-or-later":    {},
	"GPL-2.0-only":        {},
	"GPL-2.0-or-later":    {},
	"GPL-3.0-only":        {},
	"GPL-3.0-or-later":    {},
	"GPL-2.0":             {},
	"GPL-3.0":             {},
	"LGPL-2.0-only":       {},
	"LGPL-2.0-or-later":   {},
	"LGPL-2.1-only":       {},
	"LGPL-2.1-or-later":   {},
	"LGPL-3.0-only":       {},
	"LGPL-3.0-or-later":   {},
	"LGPL-2.1":            {},
	"LGPL-3.0":            {},
	"HPND":                {},
	"IJG":                 {},
	"IPA":                 {},
	"IPL-1.0":             {},
	"ISC":                 {},
	"LGPL-2.0":            {},
	"MIT":                 {},
	"MIT-0":               {},
	"MPL-1.0":             {},
	"MPL-1.1":             {},
	"MPL-2.0":             {},
	"MS-PL":               {},
	"MS-RL":               {},
	"MulanPSL-2.0":        {},
	"NCSA":                {},
	"OFL-1.1":             {},
	"OGL-UK-1.0":          {},
	"OGL-UK-2.0":          {},
	"OGL-UK-3.0":          {},
	"OLDAP-2.8":           {},
	"OpenSSL":             {},
	"OSL-1.0":             {},
	"OSL-2.0":             {},
	"OSL-3.0":             {},
	"PostgreSQL":          {},
	"Python-2.0":          {},
	"QPL-1.0":             {},
	"RPL-1.5":             {},
	"RPSL-1.0":            {},
	"RSCPL":               {},
	"Ruby":                {},
	"SISSL":               {},
	"Sleepycat":           {},
	"SPL-1.0":             {},
	"UPL-1.0":             {},
	"Unicode-DFS-2016":    {},
	"Unlicense":           {},
	"Vim":                 {},
	"W3C":                 {},
	"WTFPL":               {},
	"X11":                 {},
	"Zlib":                {},
	"ZPL-2.0":             {},
	"ZPL-2.1":             {},
}

// spdxExceptions contains official SPDX exception identifiers.
var spdxExceptions = map[string]struct{}{
	"389-exception":                  {},
	"Autoconf-exception-2.0":         {},
	"Autoconf-exception-3.0":         {},
	"Bison-exception-2.2":            {},
	"Bootloader-exception":           {},
	"Classpath-exception-2.0":        {},
	"CLISP-exception-2.0":            {},
	"DigiRule-FIRMWARE-exception":    {},
	"eCos-exception-2.0":             {},
	"Fawkes-Runtime-exception":       {},
	"FLTK-exception":                 {},
	"Font-exception-2.0":             {},
	"GCC-exception-2.0":              {},
	"GCC-exception-3.1":              {},
	"GPL-3.0-linking-exception":      {},
	"LGPL-3.0-linking-exception":     {},
	"LLVM-exception":                 {},
	"OCaml-LGPL-linking-exception":   {},
	"OpenJDK-assembly-exception-1.0": {},
	"Qwt-exception-1.0":              {},
	"Swift-exception":                {},
	"Universal-FOSS-exception-1.0":   {},
	"WxWindows-exception-3.1":        {},
}

// IsValidSPDX checks if the given string is a valid SPDX license identifier or composite expression.
// Supports boolean operators AND, OR, WITH, and parentheses.
func IsValidSPDX(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}

	tokens := tokenizeSPDX(expr)
	if len(tokens) == 0 {
		return false
	}

	p := &spdxParser{tokens: tokens, pos: 0}
	ok := p.parseExpression()
	return ok && p.pos == len(p.tokens)
}

// token types
const (
	tokIdent = iota
	tokAnd
	tokOr
	tokWith
	tokLParen
	tokRParen
)

type spdxToken struct {
	kind int
	val  string
}

func tokenizeSPDX(expr string) []spdxToken {
	var tokens []spdxToken
	runes := []rune(expr)
	n := len(runes)
	i := 0

	for i < n {
		r := runes[i]
		if unicode.IsSpace(r) {
			i++
			continue
		}
		if r == '(' {
			tokens = append(tokens, spdxToken{kind: tokLParen, val: "("})
			i++
			continue
		}
		if r == ')' {
			tokens = append(tokens, spdxToken{kind: tokRParen, val: ")"})
			i++
			continue
		}

		start := i
		for i < n && !unicode.IsSpace(runes[i]) && runes[i] != '(' && runes[i] != ')' {
			i++
		}
		word := string(runes[start:i])
		switch word {
		case "AND":
			tokens = append(tokens, spdxToken{kind: tokAnd, val: word})
		case "OR":
			tokens = append(tokens, spdxToken{kind: tokOr, val: word})
		case "WITH":
			tokens = append(tokens, spdxToken{kind: tokWith, val: word})
		default:
			tokens = append(tokens, spdxToken{kind: tokIdent, val: word})
		}
	}
	return tokens
}

type spdxParser struct {
	tokens []spdxToken
	pos    int
}

func (p *spdxParser) peek() *spdxToken {
	if p.pos < len(p.tokens) {
		return &p.tokens[p.pos]
	}
	return nil
}

func (p *spdxParser) next() *spdxToken {
	tok := p.peek()
	if tok != nil {
		p.pos++
	}
	return tok
}

func (p *spdxParser) parseExpression() bool {
	if !p.parseTerm() {
		return false
	}
	for {
		tok := p.peek()
		if tok != nil && (tok.kind == tokAnd || tok.kind == tokOr) {
			p.next()
			if !p.parseTerm() {
				return false
			}
		} else {
			break
		}
	}
	return true
}

func (p *spdxParser) parseTerm() bool {
	tok := p.peek()
	if tok == nil {
		return false
	}

	if tok.kind == tokLParen {
		p.next() // consume '('
		if !p.parseExpression() {
			return false
		}
		closeTok := p.next()
		return closeTok != nil && closeTok.kind == tokRParen
	}

	if tok.kind == tokIdent {
		p.next() // consume license ident
		licenseID := strings.TrimSuffix(tok.val, "+")
		if _, ok := spdxLicenses[licenseID]; !ok {
			return false
		}
		// check optional WITH exception
		withTok := p.peek()
		if withTok != nil && withTok.kind == tokWith {
			p.next() // consume WITH
			excTok := p.next()
			if excTok == nil || excTok.kind != tokIdent {
				return false
			}
			if _, ok := spdxExceptions[excTok.val]; !ok {
				return false
			}
		}
		return true
	}

	return false
}
