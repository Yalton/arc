package registry

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SetModule returns registry YAML data with m created or replaced in the
// modules list. Only the modules entry concerned changes: it edits the
// yaml.Node tree, so comments and the order of everything else survive.
// A new entry is appended; the modules key is created at the end of the
// file when absent. An existing entry keeps its position and its fields
// not set in m are cleared.
func SetModule(data []byte, m Module) ([]byte, error) {
	doc, root, err := parseRoot(data)
	if err != nil {
		return nil, err
	}
	seq := modulesSeq(root, true)
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
	return encodeDoc(doc)
}

// RemoveModule returns registry YAML data without the module id, and
// whether it was there. data is returned unchanged when it was not.
func RemoveModule(data []byte, id string) ([]byte, bool, error) {
	doc, root, err := parseRoot(data)
	if err != nil {
		return nil, false, err
	}
	seq := modulesSeq(root, false)
	if seq == nil {
		return data, false, nil
	}
	i := moduleIndex(seq, id)
	if i < 0 {
		return data, false, nil
	}
	seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
	if len(seq.Content) == 0 {
		seq.Style = yaml.FlowStyle
	}
	out, err := encodeDoc(doc)
	return out, true, err
}

func parseRoot(data []byte) (*yaml.Node, *yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", Path, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, errors.New(Path + ": top level is not a mapping")
	}
	return &doc, doc.Content[0], nil
}

// modulesSeq returns the modules sequence of the root mapping. With create
// it adds the key (or turns a null value into a list) when missing.
func modulesSeq(root *yaml.Node, create bool) *yaml.Node {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "modules" {
			continue
		}
		v := root.Content[i+1]
		if v.Kind == yaml.SequenceNode {
			return v
		}
		if !create {
			return nil
		}
		*v = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		return v
	}
	if !create {
		return nil
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "modules"}, seq)
	return seq
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

func encodeDoc(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
