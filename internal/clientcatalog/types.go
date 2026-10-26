package clientcatalog

import "time"

type Asset struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Arch        string `json:"arch"`
	PackageType string `json:"package_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
	Version     string `json:"version"`
	Digest      string `json:"digest,omitempty"`
}
type CachedAsset struct {
	Asset
	SHA256     string    `json:"sha256"`
	VerifiedBy string    `json:"verified_by"`
	CachedAt   time.Time `json:"cached_at"`
}
type Release struct {
	SkippedInstallers int       `json:"skipped_installers,omitempty"`
	Version           string    `json:"version"`
	URL               string    `json:"url"`
	PublishedAt       time.Time `json:"published_at"`
	Assets            []Asset   `json:"assets"`
}
type PlatformSettings struct {
	Platform    string  `json:"platform"`
	ShowHome    bool    `json:"show_home"`
	ShowPortal  bool    `json:"show_portal"`
	Recommended bool    `json:"recommended"`
	Sort        int     `json:"sort"`
	Source      string  `json:"source"`
	AssetIDs    []int64 `json:"asset_ids"`
}
type Download struct {
	ID          int64  `json:"id,omitempty"`
	Label       string `json:"label"`
	Arch        string `json:"arch,omitempty"`
	PackageType string `json:"package_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Version     string `json:"version,omitempty"`
	URL         string `json:"url"`
}
type PublishedClient struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Homepage    string     `json:"homepage"`
	Format      string     `json:"format"`
	Platform    string     `json:"platform"`
	Recommended bool       `json:"recommended"`
	Sort        int        `json:"sort"`
	Source      string     `json:"source"`
	Version     string     `json:"version,omitempty"`
	Downloads   []Download `json:"downloads"`
}

// SavedClient stores private settings and immutable publication snapshots.
// The incarnation prevents a late remote response surviving a site reset.
type SavedClient struct {
	RetainedAssets []Asset            `json:"retained_assets,omitempty"`
	Incarnation    string             `json:"incarnation"`
	Revision       int64              `json:"revision"`
	Platforms      []PlatformSettings `json:"platforms"`
	Release        *Release           `json:"release"`
	CheckedAt      *time.Time         `json:"checked_at,omitempty"`
	CheckError     string             `json:"check_error,omitempty"`
	Archived       *bool              `json:"archived,omitempty"`
	CachedAssets   []CachedAsset      `json:"cached_assets"`
	Published      []PublishedClient  `json:"published"`
}
type Client struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Homepage     string             `json:"homepage"`
	Repo         string             `json:"repo,omitempty"`
	Kind         string             `json:"kind"`
	Format       string             `json:"format"`
	Revision     int64              `json:"revision"`
	Platforms    []PlatformSettings `json:"platforms"`
	Release      *Release           `json:"release"`
	CheckedAt    *time.Time         `json:"checked_at,omitempty"`
	CheckError   string             `json:"check_error,omitempty"`
	Archived     *bool              `json:"archived,omitempty"`
	CachedAssets []CachedAsset      `json:"cached_assets"`
	Published    []PublishedClient  `json:"published"`
}
type Job struct {
	ID            string `json:"id"`
	ClientID      string `json:"client_id"`
	AssetID       int64  `json:"asset_id"`
	State         string `json:"state"`
	ReceivedBytes int64  `json:"received_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	Error         string `json:"error,omitempty"`
}
type Storage struct {
	UsedBytes     int64 `json:"used_bytes"`
	MaxBytes      int64 `json:"max_bytes"`
	MaxAssetBytes int64 `json:"max_asset_bytes"`
}
type CatalogView struct {
	Clients []Client `json:"clients"`
	Storage Storage  `json:"storage"`
	Job     *Job     `json:"job"`
}
