package schema

// topologicalSort performs a topological sort on items based on their dependencies using
// depth-first search (DFS). It returns the sorted items in dependency order, or an empty
// slice if a circular dependency is detected.
//
// The algorithm uses DFS with three-color marking (unvisited, visiting, visited) to detect
// cycles and ensure each node is processed only once.
func topologicalSort[T any](items []T, dependencies map[string][]string, getID func(T) string) []T {
	var sorted []T
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	itemMap := make(map[string]T)

	// Build item map for quick lookup
	for _, item := range items {
		id := getID(item)
		itemMap[id] = item
	}

	// DFS visit function
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			// Circular dependency detected
			return false
		}
		if visited[id] {
			return true
		}

		visiting[id] = true

		// Visit dependencies first
		for _, dep := range dependencies[id] {
			// Only visit if the dependency is in our current set of items
			if _, exists := itemMap[dep]; exists {
				if !visit(dep) {
					// Circular dependency - abandon sort
					return false
				}
			}
		}

		visiting[id] = false
		visited[id] = true

		if item, exists := itemMap[id]; exists {
			sorted = append(sorted, item)
		}
		return true
	}

	// Visit all items
	for _, item := range items {
		id := getID(item)
		if !visited[id] {
			if !visit(id) {
				// Circular dependency detected, return empty slice
				return []T{}
			}
		}
	}

	return sorted
}
