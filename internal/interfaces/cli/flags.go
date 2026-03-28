package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseLineRange parses START:END or START: format. START must be >=1. END optional or >= START.
func ParseLineRange(raw string) (int, int, error) {
	if !strings.Contains(raw, ":") { // enforce colon presence
		return 0, 0, fmt.Errorf("invalid --line-range format (expected START:END)")
	}
	parts := strings.Split(raw, ":")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid --line-range format (expected START:END)")
	}
	startStr := strings.TrimSpace(parts[0])
	if startStr == "" {
		return 0, 0, fmt.Errorf("invalid --line-range: start missing")
	}
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 1 {
		return 0, 0, fmt.Errorf("invalid --line-range: start must be integer >=1")
	}
	end := 0
	if len(parts) == 2 {
		endStr := strings.TrimSpace(parts[1])
		if endStr != "" {
			v, err := strconv.Atoi(endStr)
			if err != nil || v < start {
				return 0, 0, fmt.Errorf("invalid --line-range: end must be integer >= start")
			}
			end = v
		}
	}
	return start, end, nil
}
