// Package seed parses the YAML seed file used to bulk-import companies
// (see `swamp import`), decoupled from where the list came from or how
// it gets validated against a real job board -- that happens downstream
// in sync.ImportCompanies.
package seed

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Entry is one company row from a seed file, matching store.CreateCompany's
// params directly.
type Entry struct {
	Name      string `yaml:"name"`
	Source    string `yaml:"source"`
	SourceRef string `yaml:"source_ref"`
}

type file struct {
	Companies []Entry `yaml:"companies"`
}

// Parse reads a seed file's companies list. It only checks structural
// validity (well-formed YAML) -- whether each Source is actually
// supported, and whether SourceRef resolves to a real board, are checked
// downstream against the live fetchers map (sync.ImportCompanies), not
// here, so this package doesn't need a second hardcoded copy of the
// supported-sources list.
func Parse(r io.Reader) ([]Entry, error) {
	var f file
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&f); err != nil {
		return nil, err
	}

	for i, e := range f.Companies {
		if e.Name == "" || e.Source == "" || e.SourceRef == "" {
			return nil, fmt.Errorf("seed: entry %d: name, source, and source_ref are all required", i)
		}
	}

	return f.Companies, nil
}
