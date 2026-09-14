package store

// KomariSettings makes every managed node report to a Komari monitor as
// an agent (settings key "komari"); each node registers under its own name.
type KomariSettings struct {
	Enabled  bool   `json:"enabled"`
	Server   string `json:"server"`
	Key      string `json:"key"` // auto-discovery key; write-only in the API
	Interval int    `json:"interval"`
}

const SettingKomari = "komari"
