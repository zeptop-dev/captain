package store

// DStatusSettings makes every managed node a DStatus agent. One panel-wide
// setting: the key is the same on every node, which is what DStatus' own
// installer does too. Passive: each node listens on Listen and the panel
// scrapes it, so the port must be reachable (bosun's firewall auto-open
// opens it). Active: each node reports to Server on the interval under its
// own DStatusSID (a node field) and opens no port — for hosts the panel
// cannot reach.
type DStatusSettings struct {
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`   // "" or "passive", or "active"
	Listen   string `json:"listen"` // passive: host:port; "" = :9999
	Key      string `json:"key"`    // write-only in the API
	Server   string `json:"server"` // active: the panel's base URL
	Interval int    `json:"interval"`
}

const SettingDStatus = "dstatus"
