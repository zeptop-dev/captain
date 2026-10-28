package komari

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/probedata"
)

var errDenied = errors.New("probe access denied")

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}
type rpcResponse struct {
	Version string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcRequest struct {
	Version string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      json.RawMessage `json:"id"`
}

func invalid(msg string) *rpcError { return &rpcError{Code: -32602, Message: msg} }
func fromError(err error) *rpcError {
	if errors.Is(err, probedata.ErrNotFound) {
		return invalid("Node or metric not found")
	}
	if errors.Is(err, probedata.ErrInvalidQuery) {
		return invalid("Invalid probe query")
	}
	if errors.Is(err, probedata.ErrUnsupportedAggregation) {
		return &rpcError{Code: -32602, Message: "Requested aggregation is unavailable for stored observations", Data: map[string]string{"reason": "unsupported_aggregation"}}
	}
	return &rpcError{Code: -32603, Message: "Unable to read probe data"}
}
func errorResponse(code int, msg string) rpcResponse {
	return rpcResponse{Version: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: code, Message: msg}}
}

func (h *Handler) rpcMessage(r *http.Request, body []byte) any {
	body = bytes.TrimSpace(body)
	if !json.Valid(body) {
		return errorResponse(-32700, "Parse error")
	}
	if len(body) > 0 && body[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(body, &batch); err != nil || len(batch) == 0 || len(batch) > maxBatch {
			return errorResponse(-32600, "Batch must contain 1 to 32 requests")
		}
		out := make([]rpcResponse, 0, len(batch))
		for _, raw := range batch {
			if response := h.rpcOne(r, raw); response != nil {
				out = append(out, *response)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	if response := h.rpcOne(r, body); response != nil {
		return response
	}
	return nil
}

func (h *Handler) rpcOne(r *http.Request, body []byte) *rpcResponse {
	body = bytes.TrimSpace(body)
	bad := errorResponse(-32600, "Invalid Request")
	if len(body) == 0 || body[0] != '{' {
		return &bad
	}
	var req rpcRequest
	if json.Unmarshal(body, &req) != nil || req.Version != "2.0" || req.Method == "" || len(req.Method) > 128 {
		return &bad
	}
	if len(req.ID) > 0 {
		var id any
		dec := json.NewDecoder(bytes.NewReader(req.ID))
		dec.UseNumber()
		if dec.Decode(&id) != nil {
			return &bad
		}
		switch id.(type) {
		case nil, string, json.Number:
		default:
			return &bad
		}
	}
	result, e := h.call(r, req.Method, req.Params)
	if len(req.ID) == 0 {
		return nil
	} // a valid notification never gets a response
	if !h.allowed(r) {
		e = &rpcError{Code: -32003, Message: "Probe access denied"}
		result = nil
	}
	return &rpcResponse{Version: "2.0", ID: req.ID, Result: result, Error: e}
}

// Upstream accepts both named objects and positional arrays. Individual fields
// are decoded through encoding/json, preserving strict type/overflow checks.
func bind(raw json.RawMessage, target any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '{' {
		return json.Unmarshal(raw, target)
	}
	v := reflect.ValueOf(target).Elem()
	if v.Kind() != reflect.Struct {
		return errors.New("invalid parameters")
	}
	if raw[0] == '[' {
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		if len(values) > v.NumField() {
			return errors.New("too many parameters")
		}
		for i, value := range values {
			if err := json.Unmarshal(value, v.Field(i).Addr().Interface()); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("parameters must be an object or array")
}

func (h *Handler) call(r *http.Request, method string, raw json.RawMessage) (any, *rpcError) {
	if !h.allowed(r) {
		return nil, &rpcError{Code: -32003, Message: "Probe access denied"}
	}
	// No dynamic method registration: no admin/agent/command method can ever
	// become exposed by importing another package or by an upstream addition.
	switch method {
	case "rpc.ping", "rpc.version", "rpc.methods", "rpc.help":
		return frameworkCall(method, raw)
	case "public:getMe", "common:getMe":
		id := Identity{Username: "Guest"}
		if h.d.Identity != nil {
			id = h.d.Identity(r)
		}
		return map[string]any{"username": id.Username, "logged_in": id.LoggedIn, "uuid": id.UUID, "2fa_enabled": id.TwoFAEnabled, "sso_type": "", "sso_id": "", "captain": map[string]any{"scope": "probe-read", "is_admin": false}}, nil
	case "public:getVersion", "common:getVersion":
		return map[string]any{"version": "1.6.1", "hash": h.d.Hash, "captain": map[string]any{"version": h.d.Version, "komari_compatibility": "1.6.1", "implementation": "Captain"}}, nil
	case "public:listMetricDefinitions":
		return h.definitions(r)
	case "public:queryMetrics":
		return h.queryMetrics(r, raw)
	case "public:getPingMetricStats":
		return h.pingStats(r, raw)
	case "public:getRecordsByUUID", "public:getPingRecords", "common:getRecords":
		return h.records(r, method, raw)
	case "public:getPublicSettings", "common:getPublicInfo", "public:getNodesInformation", "common:getNodes", "common:getNodesLatestStatus", "public:getClientRecentRecords", "common:getNodeRecentStatus", "public:getPublicPingTasks":
	default:
		return nil, &rpcError{Code: -32601, Message: "Method not found"}
	}
	s, err := h.d.Reader.Snapshot(r.Context(), probedata.Scope{})
	if err != nil {
		return nil, fromError(err)
	}
	if method == "public:getPublicSettings" || method == "common:getPublicInfo" {
		var extra map[string]any
		if h.d.PublicSettings != nil {
			extra, err = h.d.PublicSettings(r)
			if err != nil {
				return nil, fromError(err)
			}
		}
		settings := h.settings(s.Settings, extra)
		defs, err := h.d.Reader.Definitions(r.Context(), probedata.Scope{})
		if err != nil {
			return nil, fromError(err)
		}
		loadDays, pingDays := 0, 0
		for _, d := range defs {
			if !d.History {
				continue
			}
			if strings.HasPrefix(d.Key, "ping.") {
				if d.RetentionDays > pingDays {
					pingDays = d.RetentionDays
				}
			} else if d.RetentionDays > loadDays {
				loadDays = d.RetentionDays
			}
		}
		settings["record_enabled"] = loadDays > 0
		settings["record_preserve_time"] = loadDays * 24
		settings["ping_record_preserve_time"] = pingDays * 24
		return settings, nil
	}
	if method == "public:getPublicPingTasks" {
		return tasksDTO(s.PingTasks), nil
	}
	var params struct {
		UUID  string   `json:"uuid"`
		UUIDs []string `json:"uuids"`
	}
	if bind(raw, &params) != nil || len(params.UUID) > 128 || len(params.UUIDs) > 128 {
		return nil, invalid("Invalid node selector")
	}
	if method == "public:getNodesInformation" {
		out := make([]map[string]any, 0, len(s.Nodes))
		for i, n := range s.Nodes {
			out = append(out, client(n, i, true))
		}
		return out, nil
	}
	if method == "public:getClientRecentRecords" || method == "common:getNodeRecentStatus" {
		n, ok := nodeByID(s, params.UUID)
		if !ok {
			return nil, invalid("Node not found")
		}
		out := make([]map[string]any, 0, len(n.Recent))
		for _, v := range n.Recent {
			if method == "public:getClientRecentRecords" {
				out = append(out, recentReport(n, v))
			} else {
				row := flatRecord(n, &v.Time, v.Values, false)
				row["connections"] = connectionTotal(v.Values)
				extension := row["captain"].(map[string]any)
				extension["connections_semantics"] = "tcp+udp"
				extension["connections_tcp"] = v.Values["connections.tcp"]
				extension["connections_udp"] = v.Values["connections.udp"]
				out = append(out, row)
			}
		}
		if method == "public:getClientRecentRecords" {
			return out, nil
		}
		return map[string]any{"count": len(out), "records": out}, nil
	}
	out := map[string]any{}
	var pingStats []probedata.PingStat
	if method == "common:getNodesLatestStatus" && len(s.PingTasks) > 0 {
		selected := []string{}
		for _, n := range s.Nodes {
			if params.UUID != "" && n.UUID != params.UUID {
				continue
			}
			if params.UUID == "" && len(params.UUIDs) > 0 && !contains(params.UUIDs, n.UUID) {
				continue
			}
			selected = append(selected, n.UUID)
		}
		if len(selected) > 0 && len(selected) <= 128 {
			// A history outage must not hide independent current measurements.
			// Missing statistics remain null in the snapshot fallback below.
			pingStats, _ = h.d.Reader.PingStats(r.Context(), probedata.Scope{}, probedata.PingQuery{EntityIDs: selected, Start: h.now().Add(-time.Hour), End: h.now()})
		}
	}
	for i, n := range s.Nodes {
		if params.UUID != "" && n.UUID != params.UUID {
			continue
		}
		if params.UUID == "" && len(params.UUIDs) > 0 && !contains(params.UUIDs, n.UUID) {
			continue
		}
		if method == "common:getNodes" {
			out[n.UUID] = client(n, i, false)
		} else {
			row := latest(n, s.PingTasks)
			ping := row["ping"].(map[string]any)
			for _, stat := range pingStats {
				if stat.EntityID != n.UUID {
					continue
				}
				ping[strconv.FormatInt(legacyTaskID(stat.TaskID, s.PingTasks), 10)] = map[string]any{"name": stat.Name, "latest": stat.Latest, "avg": stat.Avg, "tail": latencyTail(stat.P50, stat.P99, false), "loss": lossPercent(stat.Loss), "min": stat.Min, "max": stat.Max}
			}
			out[n.UUID] = row
		}
	}
	if params.UUID != "" {
		if v, ok := out[params.UUID]; ok {
			return v, nil
		}
		return nil, invalid("Node not found")
	}
	return out, nil
}

func (h *Handler) legacyMessage(r *http.Request, body string) any {
	uuid := ""
	if body != "get" {
		if !strings.HasPrefix(body, "get ") {
			return map[string]any{"status": "error", "error": "Invalid message"}
		}
		uuid = strings.TrimSpace(strings.TrimPrefix(body, "get "))
		if uuid == "" || len(uuid) > 128 {
			return map[string]any{"status": "error", "error": "Invalid message"}
		}
	}
	s, err := h.d.Reader.Snapshot(r.Context(), probedata.Scope{})
	if err != nil {
		return map[string]any{"status": "error", "error": "Unable to read probe data"}
	}
	online := []string{}
	data := map[string]any{}
	for _, n := range s.Nodes {
		if uuid != "" && n.UUID != uuid {
			continue
		}
		if n.Status.Online {
			online = append(online, n.UUID)
		}
		if n.Status.ReceivedAt != nil {
			data[n.UUID] = report(n, n.Status.ReceivedAt, n.Status.Values)
		}
	}
	return map[string]any{"status": "success", "data": map[string]any{"online": online, "data": data}}
}

// REST historically passes these values as strings; modern RPC uses numbers.
type numeric float64

func (n *numeric) UnmarshalJSON(raw []byte) error {
	var v float64
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		p, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		v = p
	} else {
		if err := json.Unmarshal(raw, &v); err != nil {
			return err
		}
	}
	*n = numeric(v)
	return nil
}
