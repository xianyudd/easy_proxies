package config

import "fmt"

// ValidateLocalSources checks cross-source invariants for a whole applied or
// draft set: per-source name validity, member limits, the 50-source cap and
// the 5000-member total cap (including disabled sources).
func ValidateLocalSources(sources []LocalSource) error {
	if len(sources) > MaxLocalSourceCount {
		return fmt.Errorf("too many local sources: %d exceeds %d", len(sources), MaxLocalSourceCount)
	}
	ids := make(map[string]struct{}, len(sources))
	total := 0
	for i, source := range sources {
		if err := ValidateLocalSourceName(source.Name); err != nil {
			return fmt.Errorf("local source %d: %w", i, err)
		}
		if source.ID == "" {
			return fmt.Errorf("local source %d: missing id", i)
		}
		if _, ok := ids[source.ID]; ok {
			return fmt.Errorf("local source %d: duplicate id %q", i, source.ID)
		}
		ids[source.ID] = struct{}{}
		if len(source.Nodes) > MaxLocalSourceNodesPerSource {
			return fmt.Errorf("local source %q exceeds %d nodes per source", source.Name, MaxLocalSourceNodesPerSource)
		}
		total += len(source.Nodes)
		if total > MaxLocalSourceNodesTotal {
			return fmt.Errorf("local sources exceed %d nodes in total", MaxLocalSourceNodesTotal)
		}
	}
	return nil
}
