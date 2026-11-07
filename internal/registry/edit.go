package registry

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SetModule returns registry YAML data with m created or replaced in the
// modules list. Only the top-level modules block is rewritten: it is found
// through the yaml.Node tree and spliced into the original bytes, so every
// byte outside it (comments, blank lines, indentation, key order) stays as
// it was. A new entry is appended; the modules key is created at the end of
// the file when absent. An existing entry keeps its position and its fields
// not set in m are cleared.
func SetModule(data []byte, m Module) ([]byte, error) {
	return editModules(data, true, func(seq *yaml.Node) bool {
		node := moduleNode(m)
		if i := moduleIndex(seq, m.ID); i >= 0 {
			node.HeadComment = seq.Content[i].HeadComment
			node.LineComment = seq.Content[i].LineComment
			node.FootComment = seq.Content[i].FootComment
			seq.Content[i] = node
		} else {
			seq.Content = append(seq.Content, node)
		}
		seq.Style &^= yaml.FlowStyle
		return true
	})
}

// RemoveModule returns registry YAML data without the module id, and
// whether it was there. data is returned unchanged when it was not.
func RemoveModule(data []byte, id string) ([]byte, bool, error) {
	removed := false
	out, err := editModules(data, false, func(seq *yaml.Node) bool {
		i := moduleIndex(seq, id)
		if i < 0 {
			return false
		}
		seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
		if len(seq.Content) == 0 {
			seq.Style = yaml.FlowStyle
		}
		removed = true
		return true
	})
	return out, removed, err
}

// editModules runs mutate on the modules sequence and splices the
// re-encoded `modules:` block over its original lines. mutate reports
// whether it changed anything; if not, data is returned as is. With create
// the key is added (at the end of the file) when absent or null.
func editModules(data []byte, create bool, mutate func(seq *yaml.Node) bool) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", Path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 ||
		doc.Content[0].Kind != yaml.MappingNode || doc.Content[0].Style&yaml.FlowStyle != 0 {
		return nil, errors.New(Path + ": top level is not a block mapping")
	}
	root := doc.Content[0]
	idx := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "modules" {
			idx = i
		}
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	if n := len(lines); n > 0 && len(lines[n-1]) == 0 {
		lines = lines[:n-1]
	}

	var key, seq *yaml.Node
	start, end := len(lines), len(lines) // absent: append at EOF
	if idx >= 0 {
		key, seq = root.Content[idx], root.Content[idx+1]
		start = key.Line - 1
		end = len(lines)
		if idx+2 < len(root.Content) {
			end = root.Content[idx+2].Line - 1
		}
		// The next key's leading blank and comment lines are not ours.
		trimmed := false
		for end > start+1 && isBlankOrComment(lines[end-1]) {
			end--
			trimmed = true
		}
		key.HeadComment, key.FootComment = "", ""
		// Trailing comment lines stay in the file, so the encoder must not
		// write them again as the last entry's (or the list's) foot.
		if trimmed && seq.Kind == yaml.SequenceNode {
			seq.FootComment = ""
			if n := len(seq.Content); n > 0 {
				clearFoot(seq.Content[n-1])
			}
		}
	}
	if seq == nil || seq.Kind != yaml.SequenceNode {
		if !create {
			return data, nil
		}
		seq = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		if key == nil {
			key = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "modules"}
		}
	}
	if !mutate(seq) {
		return data, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	block := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{key, seq}}
	if err := enc.Encode(block); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}

	// The block is written with the file's own line ending.
	eol, blockBytes := []byte("\n"), buf.Bytes()
	if bytes.Contains(data, []byte("\r\n")) {
		eol = []byte("\r\n")
		blockBytes = bytes.ReplaceAll(blockBytes, []byte("\n"), eol)
	}
	var out bytes.Buffer
	for _, l := range lines[:start] {
		out.Write(l)
	}
	if start > 0 && !bytes.HasSuffix(lines[start-1], []byte("\n")) {
		out.Write(eol)
	}
	out.Write(blockBytes)
	for _, l := range lines[end:] {
		out.Write(l)
	}
	return out.Bytes(), nil
}

// clearFoot drops foot comments in n and below; the parser may attach a
// trailing comment to any of the last entry's nested nodes.
func clearFoot(n *yaml.Node) {
	n.FootComment = ""
	for _, c := range n.Content {
		clearFoot(c)
	}
}

func isBlankOrComment(line []byte) bool {
	t := bytes.TrimSpace(line)
	return len(t) == 0 || t[0] == '#'

}

func moduleIndex(seq *yaml.Node, id string) int {
	for i, n := range seq.Content {
		if n.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == "id" && n.Content[j+1].Value == id {
				return i
			}
		}
	}
	return -1
}

// moduleNode builds the mapping for m in schema order, leaving out empty
// fields.
func moduleNode(m Module) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	add := func(key string, v *yaml.Node) { n.Content = append(n.Content, str(key), v) }
	list := func(key string, vs []string) {
		if len(vs) == 0 {
			return
		}
		s := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, v := range vs {
			s.Content = append(s.Content, str(v))
		}
		add(key, s)
	}
	one := func(key, v string) {
		if v != "" {
			add(key, str(v))
		}
	}
	one("id", m.ID)
	one("summary", m.Summary)
	list("paths", m.Paths)
	list("covers", m.Covers)
	list("entry", m.Entry)
	one("import", m.Import)
	list("rules", m.Rules)
	one("source", m.Source)
	return n
}
