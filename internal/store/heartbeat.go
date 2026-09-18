package store

import "time"

// HeartbeatSettings: settings key "heartbeat". Captain fetches URL every
// IntervalSeconds so an external watchdog notices when the panel stops.
type HeartbeatSettings struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	// IntervalSeconds between beats; 0 = 60.
	IntervalSeconds int `json:"interval_seconds"`
}

// SettingHeartbeat is the settings key.
const SettingHeartbeat = "heartbeat"

// Every returns the beat interval, one minute when unset and never below
// it: the job tick is the clock, so a shorter interval buys nothing.
func (s HeartbeatSettings) Every() time.Duration {
	if s.IntervalSeconds < 60 {
		return time.Minute
	}
	return time.Duration(s.IntervalSeconds) * time.Second
}

// HeartbeatStatus is the last attempt, for the settings card.
type HeartbeatStatus struct {
	At    time.Time `json:"at"`
	Code  int       `json:"code,omitempty"`
	Error string    `json:"error,omitempty"`
}
