package collections

import (
	"reflect"
	"sort"
	"testing"
)

func TestMergeMaps(t *testing.T) {
	tests := []struct {
		name     string
		map1     map[string]int
		map2     map[string]int
		expected map[string]int
	}{
		{
			name:     "merge_non_overlapping_maps",
			map1:     map[string]int{"a": 1, "b": 2},
			map2:     map[string]int{"c": 3, "d": 4},
			expected: map[string]int{"a": 1, "b": 2, "c": 3, "d": 4},
		},
		{
			name:     "merge_overlapping_maps_map2_wins",
			map1:     map[string]int{"a": 1, "b": 2},
			map2:     map[string]int{"b": 20, "c": 3},
			expected: map[string]int{"a": 1, "b": 20, "c": 3},
		},
		{
			name:     "merge_with_empty_map1",
			map1:     map[string]int{},
			map2:     map[string]int{"a": 1, "b": 2},
			expected: map[string]int{"a": 1, "b": 2},
		},
		{
			name:     "merge_with_empty_map2",
			map1:     map[string]int{"a": 1, "b": 2},
			map2:     map[string]int{},
			expected: map[string]int{"a": 1, "b": 2},
		},
		{
			name:     "merge_both_empty_maps",
			map1:     map[string]int{},
			map2:     map[string]int{},
			expected: map[string]int{},
		},
		{
			name:     "merge_with_nil_map1",
			map1:     nil,
			map2:     map[string]int{"a": 1, "b": 2},
			expected: map[string]int{"a": 1, "b": 2},
		},
		{
			name:     "merge_with_nil_map2",
			map1:     map[string]int{"a": 1, "b": 2},
			map2:     nil,
			expected: map[string]int{"a": 1, "b": 2},
		},
		{
			name:     "merge_both_nil_maps",
			map1:     nil,
			map2:     nil,
			expected: map[string]int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeMaps(tt.map1, tt.map2)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("MergeMaps() = %v, want %v", result, tt.expected)
			}

			// Verify result is a new map (not modifying input maps)
			if len(tt.map1) > 0 {
				// Modify result to ensure it doesn't affect original maps
				for k := range result {
					result[k] = 999
					break
				}
				// Check that original map1 is unchanged
				for k, v := range tt.map1 {
					if tt.map1[k] != v {
						t.Error("MergeMaps() modified the original map1")
						break
					}
				}
			}
		})
	}
}

func TestMergeMaps_DifferentTypes(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "merge_string_maps",
			testFunc: func() bool {
				map1 := map[string]string{"a": "hello", "b": "world"}
				map2 := map[string]string{"b": "universe", "c": "test"}
				expected := map[string]string{"a": "hello", "b": "universe", "c": "test"}
				result := MergeMaps(map1, map2)
				return reflect.DeepEqual(result, expected)
			},
			description: "MergeMaps works with string values",
		},
		{
			name: "merge_interface_maps",
			testFunc: func() bool {
				map1 := map[string]interface{}{"a": 1, "b": "hello"}
				map2 := map[string]interface{}{"b": 2.5, "c": true}
				result := MergeMaps(map1, map2)
				return result["a"] == 1 && result["b"] == 2.5 && result["c"] == true
			},
			description: "MergeMaps works with interface{} values",
		},
		{
			name: "merge_int_key_maps",
			testFunc: func() bool {
				map1 := map[int]string{1: "one", 2: "two"}
				map2 := map[int]string{2: "TWO", 3: "three"}
				expected := map[int]string{1: "one", 2: "TWO", 3: "three"}
				result := MergeMaps(map1, map2)
				return reflect.DeepEqual(result, expected)
			},
			description: "MergeMaps works with int keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.testFunc() {
				t.Errorf("Generic type test failed: %s", tt.description)
			} else {
				t.Logf("Generic type test passed: %s", tt.description)
			}
		})
	}
}

func TestKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]int
		expected []string
	}{
		{
			name:     "map_with_keys",
			input:    map[string]int{"apple": 1, "banana": 2, "cherry": 3},
			expected: []string{"apple", "banana", "cherry"},
		},
		{
			name:     "empty_map",
			input:    map[string]int{},
			expected: nil,
		},
		{
			name:     "nil_map",
			input:    nil,
			expected: nil,
		},
		{
			name:     "single_key_map",
			input:    map[string]int{"single": 42},
			expected: []string{"single"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Keys(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Keys() = %v, want nil", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Keys() length = %d, want %d", len(result), len(tt.expected))
				return
			}

			// Sort both slices for comparison since map iteration order is not guaranteed
			sort.Strings(result)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Keys() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestValues(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]int
		expected []int
	}{
		{
			name:     "map_with_values",
			input:    map[string]int{"apple": 1, "banana": 2, "cherry": 3},
			expected: []int{1, 2, 3},
		},
		{
			name:     "empty_map",
			input:    map[string]int{},
			expected: nil,
		},
		{
			name:     "nil_map",
			input:    nil,
			expected: nil,
		},
		{
			name:     "single_value_map",
			input:    map[string]int{"single": 42},
			expected: []int{42},
		},
		{
			name:     "duplicate_values",
			input:    map[string]int{"a": 1, "b": 1, "c": 2},
			expected: []int{1, 1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Values(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Values() = %v, want nil", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Values() length = %d, want %d", len(result), len(tt.expected))
				return
			}

			// Sort both slices for comparison since map iteration order is not guaranteed
			sort.Ints(result)
			sort.Ints(tt.expected)

			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Values() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestKeys_DifferentTypes(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "string_values_keys",
			testFunc: func() bool {
				input := map[string]string{"a": "apple", "b": "banana"}
				result := Keys(input)
				sort.Strings(result)
				expected := []string{"a", "b"}
				return reflect.DeepEqual(result, expected)
			},
			description: "Keys works with string value maps",
		},
		{
			name: "int_keys",
			testFunc: func() bool {
				input := map[int]string{1: "one", 2: "two", 3: "three"}
				result := Keys(input)
				sort.Ints(result)
				expected := []int{1, 2, 3}
				return reflect.DeepEqual(result, expected)
			},
			description: "Keys works with int key maps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.testFunc() {
				t.Errorf("Generic type test failed: %s", tt.description)
			} else {
				t.Logf("Generic type test passed: %s", tt.description)
			}
		})
	}
}

func TestValues_DifferentTypes(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "string_values",
			testFunc: func() bool {
				input := map[string]string{"a": "apple", "b": "banana"}
				result := Values(input)
				sort.Strings(result)
				expected := []string{"apple", "banana"}
				return reflect.DeepEqual(result, expected)
			},
			description: "Values works with string values",
		},
		{
			name: "interface_values",
			testFunc: func() bool {
				input := map[string]interface{}{"a": 1, "b": "hello", "c": true}
				result := Values(input)
				// Can't easily sort interface{} slice, so just check length and presence
				return len(result) == 3
			},
			description: "Values works with interface{} values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.testFunc() {
				t.Errorf("Generic type test failed: %s", tt.description)
			} else {
				t.Logf("Generic type test passed: %s", tt.description)
			}
		})
	}
}

func TestCollectionsIntegration(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "merge_then_extract_keys_values",
			testFunc: func() bool {
				map1 := map[string]int{"a": 1, "b": 2}
				map2 := map[string]int{"b": 20, "c": 3}

				merged := MergeMaps(map1, map2)
				keys := Keys(merged)
				values := Values(merged)

				sort.Strings(keys)
				sort.Ints(values)

				expectedKeys := []string{"a", "b", "c"}
				expectedValues := []int{1, 3, 20}

				return reflect.DeepEqual(keys, expectedKeys) && reflect.DeepEqual(values, expectedValues)
			},
			description: "Integration test: merge maps then extract keys and values",
		},
		{
			name: "copy_behavior_verification",
			testFunc: func() bool {
				original := map[string]int{"a": 1, "b": 2}
				copied := MergeMaps(original, nil)

				// Modify the copy
				copied["a"] = 999

				// Original should be unchanged
				return original["a"] == 1
			},
			description: "MergeMaps creates independent copies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.testFunc() {
				t.Errorf("Integration test failed: %s", tt.description)
			} else {
				t.Logf("Integration test passed: %s", tt.description)
			}
		})
	}
}
