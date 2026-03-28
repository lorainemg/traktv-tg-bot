package tmdb

// Provider represents a single streaming service (e.g. Netflix, HBO Max).
type Provider struct {
	Name string `json:"provider_name"`
}

// CountryProviders holds the watch options for a specific country.
type CountryProviders struct {
	Link     string     `json:"link"`     // JustWatch URL for this show in that country
	Flatrate []Provider `json:"flatrate"` // subscription streaming services
}

// watchProvidersResponse is the raw JSON shape from TMDB.
// "results" is a map keyed by country code (e.g. "US", "GB").
type watchProvidersResponse struct {
	Results map[string]CountryProviders `json:"results"`
}

// ProviderInfo holds a streaming service name and its homepage URL (if known).
type ProviderInfo struct {
	Name string // e.g. "Netflix"
	URL  string // e.g. "https://www.netflix.com", empty if unknown
}

// WatchInfo is what we return to callers — a clean summary of where to watch.
type WatchInfo struct {
	Providers []ProviderInfo // streaming services with links
	Link      string         // JustWatch URL for this specific show
}

// providerLinks maps TMDB provider_name values to their homepage URLs.
// Names must match TMDB's exact strings — most use "Plus" spelled out,
// but some like "AMC+" keep the symbol.
var providerLinks = map[string]string{
	"Netflix":                      "https://www.netflix.com",
	"Amazon Prime Video":           "https://www.primevideo.com",
	"Amazon Prime Video with Ads":  "https://www.primevideo.com",
	"Disney Plus":                  "https://www.disneyplus.com",
	"HBO Max":                      "https://www.hbomax.com",
	"Hulu":                         "https://www.hulu.com",
	"Apple TV Plus":                "https://tv.apple.com",
	"Paramount Plus":               "https://www.paramountplus.com",
	"Peacock":                      "https://www.peacocktv.com",
	"Peacock Premium":              "https://www.peacocktv.com",
	"Crunchyroll":                  "https://www.crunchyroll.com",
	"fuboTV":                       "https://www.fubo.tv",
	"YouTube TV":                   "https://tv.youtube.com",
	"Starz":                        "https://www.starz.com",
	"AMC+":                         "https://www.amcplus.com",
}
