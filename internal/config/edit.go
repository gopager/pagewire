package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SetSourceRTLSDR writes key: value under sources[name].rtlsdr in the YAML
// file at path, keeping comments and formatting of everything else.
func SetSourceRTLSDR(path, name, key string, value any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	root := doc.Content[0]
	sources := mapValue(root, "sources")
	if sources == nil || sources.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s: no sources list", path)
	}
	var src *yaml.Node
	for _, item := range sources.Content {
		n := mapValue(item, "name")
		if (n != nil && n.Value == name) || (n == nil && len(sources.Content) == 1) {
			src = item
			break
		}
	}
	if src == nil {
		return fmt.Errorf("%s: source %q not found", path, name)
	}
	rtl := mapValue(src, "rtlsdr")
	if rtl == nil {
		rtl = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		src.Content = append(src.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rtlsdr"}, rtl)
	}
	val := &yaml.Node{Kind: yaml.ScalarNode}
	if err := val.Encode(value); err != nil {
		return err
	}
	if existing := mapValue(rtl, key); existing != nil {
		existing.Value, existing.Tag, existing.Style = val.Value, val.Tag, 0
	} else {
		rtl.Content = append(rtl.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, val)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// mapValue returns the value node of key in a mapping node, or nil.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
