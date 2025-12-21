// Package utils contains small helper functions used across the project.
//
// These are usually generic helpers that don't belong to a specific domain.
package utils

import (
	"encoding/json"
	"fmt"
)

// PrintJSON pretty-prints any Go value as indented JSON to stdout.
//
// Useful for debugging structs and responses.
// If the value contains unsupported types (channels, funcs, circular refs),
// json.MarshalIndent will return an error.
func PrintJSON(v interface{}) {
	// MarshalIndent serializes v into JSON with indentation.
	// Parameters:
	//   - v: any Go value
	//   - prefix: "" means no prefix at each line
	//   - indent: "\t" means each indentation level uses a tab
	//
	// NOTE: The variable name `json` here shadows the imported `json` package name.
	// It works because you don't use the package name after this line,
	// but it's a confusing naming choice.
	json, err := json.MarshalIndent(v, "", "	")

	if err != nil {
		// Prints a human-readable message and exits early.
		fmt.Println("Error marshalling the JSON:", err)
		return
	}

	// Print the JSON string.
	fmt.Println("JSON:", string(json))
}
