package main

import (
	"fmt"
	"strings"
)

// Helper function to create an int32 pointer
func int32p(i int32) *int32 {
	r := new(int32)
	*r = i
	return r
}

// Helper function to build a string of keys in the format
// of key=value, delimited by commas
func keysString(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		keys = append(keys, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(keys, ",")
}
