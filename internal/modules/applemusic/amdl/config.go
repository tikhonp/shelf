// Package amdl runs the upstream amdl binary for the applemusic module: it
// renders amdl's config from the base file, runs one download, and reads
// amdl's output. amdl's repository has no licence, so this package copies
// none of its code; the output it reads is what amdl prints.
package amdl

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"
)

// Version is the amdl in shelf's image. Bump it with the Dockerfile's.
const Version = "0.1.4"

// Base is what shelf itself reads of the base config.
type Base struct {
	// Language is general.language, sent to Apple's catalog as l.
	Language string
	// Storefront is general.storefront, used when the wrapper can't say.
	Storefront string
}

// Settings are the keys shelf sets in amdl's config.
type Settings struct {
	// LiteServer is general.lite-server: the wrapper's address.
	LiteServer string
	// Folder is where every kind of download lands: paths.alac, atmos, aac
	// and mv.
	Folder string
}

// Render returns the base config with shelf's keys set. Every other key
// keeps its value, its place and its comments.
func Render(base []byte, s Settings) ([]byte, Base, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, Base{}, fmt.Errorf("not valid YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, Base{}, errors.New("not a YAML mapping of amdl's sections")
	}
	root := doc.Content[0]
	general, err := section(root, "general")
	if err != nil {
		return nil, Base{}, err
	}
	paths, err := section(root, "paths")
	if err != nil {
		return nil, Base{}, err
	}
	b := Base{Language: scalar(general, "language"), Storefront: scalar(general, "storefront")}
	set(general, "lite-server", str(s.LiteServer))
	// Otherwise a failed track makes amdl wait on stdin and run the whole
	// queue again.
	set(general, "exit-on-error", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
	// A proxy here would carry the wrapper's traffic too.
	set(general, "proxy", str(""))
	for _, k := range []string{"alac", "atmos", "aac", "mv"} {
		set(paths, k, str(s.Folder))
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, Base{}, err
	}
	if err := enc.Close(); err != nil {
		return nil, Base{}, err
	}
	return out.Bytes(), b, nil
}

func str(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v, Style: yaml.DoubleQuotedStyle}
}

// section finds a top-level mapping, adding it when it is missing.
func section(root *yaml.Node, key string) (*yaml.Node, error) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			v := root.Content[i+1]
			if v.Kind == yaml.ScalarNode && v.Tag == "!!null" {
				*v = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			}
			if v.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("%s is not a mapping", key)
			}
			return v, nil
		}
	}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
	return v, nil
}

// set replaces a key's value, keeping the comment beside it.
func set(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			old := m.Content[i+1]
			v.LineComment, v.HeadComment, v.FootComment = old.LineComment, old.HeadComment, old.FootComment
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

func scalar(m *yaml.Node, key string) string {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key && m.Content[i+1].Kind == yaml.ScalarNode {
			return m.Content[i+1].Value
		}
	}
	return ""
}

// Renderer keeps the rendered config in step with the base file.
type Renderer struct {
	// BasePath is SHELF_AMDL_CONFIG; Path is where amdl reads its config.
	BasePath string
	Path     string
	Settings Settings

	mu      sync.Mutex
	written stamp
	base    Base
}

type stamp struct {
	mod  time.Time
	size int64
}

// Load reads and checks the base config without writing anything. Init
// calls it, so a broken file turns Apple Music off at once.
func (r *Renderer) Load() (Base, error) {
	data, err := os.ReadFile(r.BasePath)
	if err != nil {
		return Base{}, fmt.Errorf("the amdl config %s can't be read: %w", r.BasePath, err)
	}
	_, b, err := Render(data, r.Settings)
	if err != nil {
		return Base{}, fmt.Errorf("the amdl config %s is broken: %w", r.BasePath, err)
	}
	return b, nil
}

// Ensure writes the config with mode 0600 when the base file changed since
// the last write, or the config is missing, and returns the base.
func (r *Renderer) Ensure() (Base, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fi, err := os.Stat(r.BasePath)
	if err != nil {
		return Base{}, fmt.Errorf("the amdl config %s can't be read: %w", r.BasePath, err)
	}
	now := stamp{fi.ModTime(), fi.Size()}
	if _, err := os.Stat(r.Path); err == nil && now == r.written {
		return r.base, nil
	}
	data, err := os.ReadFile(r.BasePath)
	if err != nil {
		return Base{}, fmt.Errorf("the amdl config %s can't be read: %w", r.BasePath, err)
	}
	out, b, err := Render(data, r.Settings)
	if err != nil {
		return Base{}, fmt.Errorf("the amdl config %s is broken: %w", r.BasePath, err)
	}
	if err := writeFile(r.Path, out); err != nil {
		return Base{}, err
	}
	r.written, r.base = now, b
	return b, nil
}

// writeFile replaces path through a temporary file, with mode 0600: the
// config holds amdl's media-user-token.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
