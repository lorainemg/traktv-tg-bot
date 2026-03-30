package data

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// go:embed is a compiler directive that reads a file at build time and stores
// its contents in the variable. The file becomes part of the compiled binary —
// no need to ship it separately or worry about file paths at runtime.
// The blank import of "embed" above is required to enable this feature.
//
//go:embed timezones.json
var timezonesJSON []byte

// countryTimezones is populated once at startup from the embedded JSON.
// Maps ISO 3166-1 alpha-2 country codes to their IANA timezone names.
// Source: IANA zone1970.tab (public domain).
var countryTimezones map[string][]string

// init runs automatically when the package is loaded — before main() starts.
// Like Python's module-level code or C#'s static constructors.
// We use it to parse the embedded JSON once, so lookups are just map reads.
func init() {
	if err := json.Unmarshal(timezonesJSON, &countryTimezones); err != nil {
		panic(fmt.Sprintf("failed to parse embedded timezones.json: %v", err))
	}
}

// GetTimezonesForCountry returns the IANA timezone names for a country code.
// Returns nil if the country is not found.
func GetTimezonesForCountry(country string) []string {
	return countryTimezones[country]
}
