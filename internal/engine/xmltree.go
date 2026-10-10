package engine

import (
	"encoding/xml"
	"io"
	"strings"
)

// Node is a generic XML element. Tally responses vary in shape between
// releases and between report and collection exports, so the engines walk a
// tree instead of binding to rigid structs.
type Node struct {
	Name     string
	Attrs    map[string]string
	Children []*Node
	Text     string
}

func parseXML(data string) (*Node, error) {
	dec := xml.NewDecoder(strings.NewReader(sanitizeXML(data)))
	dec.Strict = false

	root := &Node{Name: "#root", Attrs: map[string]string{}}
	stack := []*Node{root}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Return what we have: a partially parsed Tally response is still
			// more useful than nothing, and the caller can surface Raw.
			return root, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &Node{Name: strings.ToUpper(t.Name.Local), Attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.Attrs[strings.ToUpper(a.Name.Local)] = a.Value
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			cur := stack[len(stack)-1]
			cur.Text += string(t)
		}
	}
	return root, nil
}

// Control runes, compared numerically so this file needs no escape sequences.
const (
	runeBOM   = 0xFEFF
	runeTab   = 0x09
	runeLF    = 0x0A
	runeCR    = 0x0D
	runeSpace = 0x20
)

// sanitizeXML removes characters that are illegal in XML 1.0. Tally emits
// control characters and stray nulls that would otherwise abort the decoder.
func sanitizeXML(s string) string {
	s = strings.TrimPrefix(s, string(rune(runeBOM)))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == 0 {
			continue
		}
		if r < runeSpace && r != runeTab && r != runeLF && r != runeCR {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (n *Node) Attr(name string) string {
	if n == nil || n.Attrs == nil {
		return ""
	}
	return n.Attrs[strings.ToUpper(name)]
}

func (n *Node) Child(name string) *Node {
	if n == nil {
		return nil
	}
	up := strings.ToUpper(name)
	for _, c := range n.Children {
		if c.Name == up {
			return c
		}
	}
	return nil
}

// Collect returns every descendant with the given name, in document order.
func (n *Node) Collect(name string, out *[]*Node) {
	if n == nil {
		return
	}
	up := strings.ToUpper(name)
	for _, c := range n.Children {
		if c.Name == up {
			*out = append(*out, c)
		}
		c.Collect(name, out)
	}
}

func (n *Node) FindAll(name string) []*Node {
	var out []*Node
	n.Collect(name, &out)
	return out
}

// LeafText returns the trimmed text of a direct child, or of this node.
func (n *Node) LeafText(name string) string {
	if c := n.Child(name); c != nil {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

// Value returns the first non-empty of: a NAME attribute, the text of a child
// with that name, or this node's own text.
func (n *Node) Value(name string) string {
	if v := n.Attr(name); v != "" {
		return v
	}
	if v := n.LeafText(name); v != "" {
		return v
	}
	if n.Attr("NAME") != "" {
		return n.Attr("NAME")
	}
	return strings.TrimSpace(n.Text)
}
