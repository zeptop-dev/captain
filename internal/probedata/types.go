// Package probedata owns the public, transport-independent probe projection.
// Only explicitly named fields cross this boundary; collector structs do not.
package probedata

import (
	"context"
	"errors"
	"time"

	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

var ErrNotFound = errors.New("probe data not found")
var ErrInvalidQuery = errors.New("invalid probe query")
var ErrUnsupportedAggregation = errors.New("aggregation unavailable for stored observations")

// Scope never grants hidden nodes or private IPs. Administrative inspection
// belongs to the existing management API, not third-party theme JavaScript.
type Scope struct{ Staff bool }
type Reader interface {
	Snapshot(context.Context, Scope) (Snapshot, error)
	Definitions(context.Context, Scope) ([]Definition, error)
	Query(context.Context, Scope, Query) ([]Series, error)
	PingStats(context.Context, Scope, PingQuery) ([]PingStat, error)
}
type Engine struct {
	Store    *store.Store
	Probe    *service.Probe
	SiteName string
	Now      func() time.Time
}

type Snapshot struct {
	Schema    string     `json:"schema"`
	Now       time.Time  `json:"now"`
	Settings  Settings   `json:"settings"`
	Nodes     []Node     `json:"nodes"`
	PingTasks []PingTask `json:"ping_tasks"`
}
type Settings struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Logo           string   `json:"logo"`
	LogoDark       string   `json:"logo_dark"`
	Favicon        string   `json:"favicon"`
	PageTitle      string   `json:"page_title"`
	Visibility     string   `json:"visibility"`
	Enabled        bool     `json:"enabled"`
	BeatSeconds    int      `json:"beat_seconds"`
	OfflineSeconds int      `json:"offline_seconds"`
	Sections       []string `json:"sections"`
}
type Node struct {
	ID        int64     `json:"-"`
	UUID      string    `json:"uuid"`
	Name      string    `json:"name"`
	Group     string    `json:"group"`
	Inventory Inventory `json:"inventory"`
	Status    Status    `json:"status"`
	Traffic   *Traffic  `json:"traffic,omitempty"`
	Info      *Info     `json:"info,omitempty"`
	Storage   []Storage `json:"storage"`
	Pings     []Ping    `json:"pings"`
	Recent    []Recent  `json:"recent"`
}
type Inventory struct {
	Address            string  `json:"address,omitempty"`
	IPv4               string  `json:"ipv4,omitempty"`
	IPv6               string  `json:"ipv6,omitempty"`
	CPUModel           string  `json:"cpu_model,omitempty"`
	CPUCores           int     `json:"cpu_cores,omitempty"`
	CPUPhysicalCores   int     `json:"cpu_physical_cores,omitempty"`
	CPUSockets         int     `json:"cpu_sockets,omitempty"`
	OS                 string  `json:"os,omitempty"`
	Kernel             string  `json:"kernel,omitempty"`
	Arch               string  `json:"arch,omitempty"`
	Virtualization     string  `json:"virtualization,omitempty"`
	VirtualizationRole string  `json:"virtualization_role,omitempty"`
	Version            string  `json:"version,omitempty"`
	MemTotal           *uint64 `json:"mem_total"`
	SwapTotal          *uint64 `json:"swap_total"`
	DiskTotal          *uint64 `json:"disk_total"`
}
type Status struct {
	Online     bool       `json:"online"`
	LastSeen   *time.Time `json:"last_seen"`
	ReceivedAt *time.Time `json:"received_at"`
	Fresh      bool       `json:"fresh"`
	// Values use Definition keys and units. Null means not observed or hidden;
	// stale accepted observations retain their timestamp and Fresh=false.
	Values map[string]*float64 `json:"values"`
}
type Info struct {
	Region      string   `json:"region,omitempty"`
	Provider    string   `json:"provider,omitempty"`
	ProviderURL string   `json:"provider_url,omitempty"`
	Note        string   `json:"note,omitempty"`
	Price       string   `json:"price,omitempty"`
	ExpiresAt   string   `json:"expires_at,omitempty"`
	Cost        *Cost    `json:"cost,omitempty"`
	AutoRenewal *bool    `json:"auto_renewal,omitempty"`
	Latitude    *float64 `json:"latitude,omitempty"`
	Longitude   *float64 `json:"longitude,omitempty"`
}
type Cost struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Cycle    string `json:"cycle"`
	Years    string `json:"years,omitempty"`
}
type Traffic struct {
	Valid      bool   `json:"valid"`
	UsedUp     int64  `json:"used_up"`
	UsedDown   int64  `json:"used_down"`
	Used       int64  `json:"used"`
	Limit      int64  `json:"limit"`
	Mode       string `json:"mode"`
	CycleStart *int64 `json:"cycle_start"`
	CycleEnd   *int64 `json:"cycle_end"`
	UpdatedAt  *int64 `json:"updated_at"`
}
type Storage struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"`
	Total *uint64 `json:"total"`
	Used  *uint64 `json:"used"`
}
type Recent struct {
	Time   time.Time           `json:"time"`
	Values map[string]*float64 `json:"values"`
}
type PingTask struct {
	ID        string   `json:"id"`
	LegacyID  int64    `json:"-"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Interval  int      `json:"interval"`
	Series    string   `json:"series,omitempty"`
	Family    string   `json:"family,omitempty"`
	EntityIDs []string `json:"entity_ids"`
}
type Ping struct {
	TaskID  string       `json:"task_id"`
	Name    string       `json:"name"`
	Time    *time.Time   `json:"time"`
	Latency *float64     `json:"latency_ms"`
	Loss    *float64     `json:"loss_percent"`
	State   string       `json:"state"`
	Reason  string       `json:"reason,omitempty"`
	Quality *PingQuality `json:"quality,omitempty"`
}
type PingQuality struct {
	Attempts  int            `json:"attempts"`
	Successes int            `json:"successes"`
	Outcomes  map[string]int `json:"outcomes,omitempty"`
	Min       *float64       `json:"min_ms"`
	Max       *float64       `json:"max_ms"`
	P50       *float64       `json:"p50_ms"`
	P95       *float64       `json:"p95_ms"`
	Jitter    *float64       `json:"jitter_ms"`
	Skipped   uint64         `json:"skipped"`
}
type Definition struct {
	Key           string   `json:"key"`
	Type          string   `json:"type"`
	Unit          string   `json:"unit"`
	Category      string   `json:"category"`
	RetentionDays int      `json:"retention_days"`
	History       bool     `json:"history"`
	Aggregations  []string `json:"aggregations"`
}
type Query struct {
	MetricKeys          []string
	EntityIDs           []string
	Start               time.Time
	End                 time.Time
	Tags                map[string]string
	FillEmpty           bool
	MaxPoints           int
	MaxPointsByMetric   map[string]int
	Aggregation         string
	AggregationByMetric map[string]string
}
type Point struct {
	Time   *time.Time        `json:"time"`
	Value  *float64          `json:"value"`
	Count  int               `json:"count,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}
type Series struct {
	MetricKey       string            `json:"metric_key"`
	EntityID        string            `json:"entity_id"`
	Type            string            `json:"type"`
	Unit            string            `json:"unit"`
	RetentionDays   int               `json:"retention_days"`
	Tags            map[string]string `json:"tags,omitempty"`
	Downsampled     bool              `json:"downsampled"`
	Aggregation     string            `json:"downsample_algorithm"`
	FillEmpty       bool              `json:"fill_empty"`
	MaxPoints       int               `json:"max_points"`
	IntervalSeconds int               `json:"interval_seconds"`
	Count           int               `json:"count"`
	Points          []Point           `json:"points"`
}
type PingQuery struct {
	EntityIDs []string
	TaskIDs   []string
	Start     time.Time
	End       time.Time
}
type PingStat struct {
	EntityID        string            `json:"entity_id"`
	TaskID          string            `json:"task_id"`
	Name            string            `json:"name"`
	Type            string            `json:"type"`
	Interval        int               `json:"interval"`
	Tags            map[string]string `json:"tags,omitempty"`
	Total           int               `json:"total"`
	Valid           int               `json:"valid"`
	Loss            *float64          `json:"loss"`
	LossApproximate bool              `json:"loss_approximate"`
	Min             *float64          `json:"min"`
	Max             *float64          `json:"max"`
	Avg             *float64          `json:"avg"`
	Latest          *float64          `json:"latest"`
	P50             *float64          `json:"p50"`
	P99             *float64          `json:"p99"`
	Stddev          *float64          `json:"stddev"`
	P99P50Ratio     *float64          `json:"p99_p50_ratio"`
	Quality         *PingQuality      `json:"captain_quality,omitempty"`
}
