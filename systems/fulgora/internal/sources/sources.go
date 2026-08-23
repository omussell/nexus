// Package sources wires up every Fulgora dataset source. To add a new
// source: implement the source.Source interface in its own package under
// internal/source/<name> and add a single construction there to the map below.
package sources

import (
	"fmt"
	"sort"

	"github.com/nexus/fulgora/internal/source"
	"github.com/nexus/fulgora/internal/source/retractionwatch"
	"github.com/nexus/fulgora/internal/source/ror"
)

var registry = map[string]func() source.Source{
	"ror":             func() source.Source { return ror.New() },
	"retractionwatch": func() source.Source { return retractionwatch.New() },
}

// All returns the names of every registered source, sorted alphabetically.
func All() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get returns the source registered under name, or an error if none exists.
func Get(name string) (source.Source, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("source %q is not registered; available: %v", name, All())
	}
	return factory(), nil
}
