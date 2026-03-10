// Package collections provides generic utility functions for working with
// maps, slices, and other collections to avoid code duplication
package collections

// MergeMaps merges two maps of the same type, with values from map2 overwriting
// values from map1 when keys conflict
// This replaces type-specific merge functions throughout the codebase
func MergeMaps[K comparable, V any](map1, map2 map[K]V) map[K]V {
	if map1 == nil && map2 == nil {
		return make(map[K]V)
	}
	if map1 == nil {
		return copyMap(map2)
	}
	if map2 == nil {
		return copyMap(map1)
	}

	result := make(map[K]V, len(map1)+len(map2))
	for k, v := range map1 {
		result[k] = v
	}
	for k, v := range map2 {
		result[k] = v
	}
	return result
}

// copyMap creates a shallow copy of a map
func copyMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return make(map[K]V)
	}

	result := make(map[K]V, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// Keys returns all keys from a map as a slice
func Keys[K comparable, V any](m map[K]V) []K {
	if len(m) == 0 {
		return nil
	}

	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Values returns all values from a map as a slice
func Values[K comparable, V any](m map[K]V) []V {
	if len(m) == 0 {
		return nil
	}

	values := make([]V, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	return values
}
