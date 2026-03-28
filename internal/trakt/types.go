package trakt

// CalendarEntry represents one item from the Trakt calendar API response.
type CalendarEntry struct {
	FirstAired string  `json:"first_aired"`
	Episode    Episode `json:"episode"`
	Show       Show    `json:"show"`
}

type Episode struct {
	Season int    `json:"season"`
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type Show struct {
	Title   string     `json:"title"`
	Genres  []string   `json:"genres"`  // e.g. ["anime", "drama"] — lowercase strings from extended=full
	IDs     ShowIDs    `json:"ids"`     // external identifiers — populated when using ?extended=full
	Rating  float64    `json:"rating"`  // Trakt community rating (0-10), also from extended=full
	Runtime int        `json:"runtime"` // typical episode length in minutes — from extended=full
	Images  ShowImages `json:"images"`  // poster, fanart, etc. — from extended=full
}

// ShowImages holds image URLs returned by Trakt when using ?extended=full.
// Each field is a slice because Trakt may return multiple URLs per type.
// The URLs are missing the "https://" prefix — we prepend it when using them.
type ShowImages struct {
	Poster []string `json:"poster"`
	Thumb  []string `json:"thumb"`
}

// WatchlistEntry represents one item from the Trakt watchlist API response.
// We only need the show's IDs to build an exclusion set.
type WatchlistEntry struct {
	Show Show `json:"show"`
}

// ShowIDs holds the cross-platform identifiers that Trakt returns for a show.
// We need TMDB to call the TMDB watch-providers API later.
type ShowIDs struct {
	Trakt int    `json:"trakt"`
	Slug  string `json:"slug"`
	IMDB  string `json:"imdb"`
	TMDB  int    `json:"tmdb"`
	TVDB  int    `json:"tvdb"`
}

// DeviceCode holds the response from POST /oauth/device/code.
// The user visits VerificationURL and enters UserCode to authorize.
type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"` // polling interval in seconds
}

// Token holds the OAuth tokens returned after successful authorization.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	CreatedAt    int    `json:"created_at"`
}
