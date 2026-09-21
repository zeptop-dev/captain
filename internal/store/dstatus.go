package store

// DStatusSettings makes every managed node answer a DStatus panel's
// scrapes as a neko-status agent. One panel-wide setting: the port and
// the key are the same on every node, which is what DStatus' own
// installer does, and each server is told apart by its address.
//
// Unlike Komari this is a pull, so the port has to be reachable from the
// DStatus panel; bosun's firewall auto-open opens it while it is on.
type DStatusSettings struct {
	Enabled bool   `json:"enabled"`
	Listen  string `json:"listen"` // host:port; "" = :9999
	Key     string `json:"key"`    // write-only in the API
}

const SettingDStatus = "dstatus"
