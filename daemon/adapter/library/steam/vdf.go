package steam

import (
	"fmt"
	"strings"
)

// node is one value of Valve's KeyValues text format: either a leaf string or
// an object.
//
// Valve's own parser is lenient and so is this one, on purpose. These files are
// not a contract anybody promised us: they are Steam's internal bookkeeping,
// they have changed shape at least once (see [libraryPaths]), and the only cost
// of failing to read one is that a game does not float to the top of a list the
// user can scroll anyway. Being strict here would turn a cosmetic feature into
// a startup error.
type node struct {
	leaf     string
	children map[string]*node
	// order keeps the keys as they appeared. The library file numbers its
	// entries, and reading them out of order would silently reshuffle which
	// disk a game is claimed to be on.
	order []string
}

func (n *node) isObject() bool { return n.children != nil }

// child returns the named child, nil when absent or when this is a leaf.
func (n *node) child(key string) *node {
	if n == nil || n.children == nil {
		return nil
	}
	return n.children[key]
}

// str returns the leaf value of the named child, empty when it is missing or
// is an object.
func (n *node) str(key string) string {
	c := n.child(key)
	if c == nil || c.isObject() {
		return ""
	}
	return c.leaf
}

// parseKeyValues reads Valve's KeyValues text into a tree.
//
// The whole grammar is three things: quoted strings, braces, and comments.
// Everything else is whitespace. There is no escaping worth honouring beyond
// the backslash pairs Windows paths carry.
func parseKeyValues(src string) (*node, error) {
	p := &parser{src: src}
	root := &node{children: map[string]*node{}}
	if err := p.readInto(root, 0); err != nil {
		return nil, err
	}
	return root, nil
}

type parser struct {
	src string
	i   int
}

// maxDepth stops a malformed file from recursing until the stack gives out.
// Real files nest three or four levels; anything past this is not a file we
// need to read.
const maxDepth = 32

func (p *parser) readInto(dst *node, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("vdf: demasiados niveles anidados")
	}
	for {
		p.skipBlanks()
		if p.i >= len(p.src) {
			return nil
		}
		if p.src[p.i] == '}' {
			p.i++
			return nil
		}

		key, err := p.readString()
		if err != nil {
			return err
		}

		p.skipBlanks()
		if p.i >= len(p.src) {
			// A key with nothing after it. The file is truncated; keep what
			// was read rather than throwing the whole library away.
			return nil
		}

		child := &node{}
		if p.src[p.i] == '{' {
			p.i++
			child.children = map[string]*node{}
			if err := p.readInto(child, depth+1); err != nil {
				return err
			}
		} else {
			v, err := p.readString()
			if err != nil {
				return err
			}
			child.leaf = v
		}

		// Last one wins on a repeated key, which is what Valve's parser does.
		// The key is only appended to the order once so it cannot be visited
		// twice.
		if _, seen := dst.children[key]; !seen {
			dst.order = append(dst.order, key)
		}
		dst.children[key] = child
	}
}

// skipBlanks eats whitespace and `//` comments.
func (p *parser) skipBlanks() {
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.i++
		case c == '/' && p.i+1 < len(p.src) && p.src[p.i+1] == '/':
			for p.i < len(p.src) && p.src[p.i] != '\n' {
				p.i++
			}
		default:
			return
		}
	}
}

// readString reads one token, quoted or bare.
//
// Bare tokens are not in any spec Valve published; they show up anyway, and
// accepting them costs four lines.
func (p *parser) readString() (string, error) {
	if p.i >= len(p.src) {
		return "", fmt.Errorf("vdf: se acabó el archivo donde se esperaba un valor")
	}
	if p.src[p.i] != '"' {
		start := p.i
		for p.i < len(p.src) && !isBlank(p.src[p.i]) && p.src[p.i] != '{' && p.src[p.i] != '}' {
			p.i++
		}
		if start == p.i {
			return "", fmt.Errorf("vdf: carácter inesperado %q", p.src[p.i])
		}
		return p.src[start:p.i], nil
	}

	p.i++ // the opening quote
	var b strings.Builder
	for p.i < len(p.src) {
		c := p.src[p.i]
		switch c {
		case '"':
			p.i++
			return b.String(), nil
		case '\\':
			// `\\` is the one that matters: every Windows path in these files
			// arrives doubled. The rest are passed through as written, because
			// guessing at escapes Valve may not use would corrupt paths.
			if p.i+1 < len(p.src) {
				switch p.src[p.i+1] {
				case '\\', '"':
					b.WriteByte(p.src[p.i+1])
					p.i += 2
					continue
				}
			}
			b.WriteByte(c)
			p.i++
		default:
			b.WriteByte(c)
			p.i++
		}
	}
	return "", fmt.Errorf("vdf: una cadena se quedó sin cerrar")
}

func isBlank(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}
