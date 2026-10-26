package domain

const (
	AccessAll    = "all"
	AccessGroups = "groups"
	AccessNone   = "none"
)

// InboundAccess controls real authentication, shared by every Entry pointing at
// the same listener. It is independent of display categories and user blocks.
type InboundAccess struct {
	InboundID int64   `json:"inbound_id"`
	Policy    string  `json:"policy"`
	GroupIDs  []int64 `json:"group_ids"`
	Legacy    bool    `json:"legacy"`
}

type InboundAccessChange struct {
	InboundID     int64   `json:"inbound_id"`
	Policy        string  `json:"policy"`
	GroupIDs      []int64 `json:"group_ids"`
	Revision      string  `json:"revision"`
	ConfirmShared bool    `json:"confirm_shared"`
}
