package search

import (
	"strconv"
	"strings"
)

func embedLiteral(vector []float32) string {
	if len(vector) == 0 {
		return "[]"
	}
	parts := make([]string, len(vector))
	for i, v := range vector {
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
