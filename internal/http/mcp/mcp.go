// Package mcp exposes Captain to AI agents over the Model Context Protocol
// (JSON-RPC 2.0 over streamable HTTP, POST /mcp). Tools reuse the store and
// services behind the admin API; the caller's API token decides the role,
// and write tools additionally require `confirm: true`.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/service"
	"github.com/zeptop-dev/captain/internal/store"
)

const protocolVersion = "2025-06-18"

// Deps are the handler dependencies.
type Deps struct {
	Store   *store.Store
	Probe   *service.Probe
	Log     *slog.Logger
	Version string
	// Resolve turns a bearer token into a staff user, nil when invalid.
	Resolve func(ctx context.Context, token string) *domain.User
	// NewUser builds a user record (password hashing lives in the admin package).
	NewUser func(email, password string) (*domain.User, error)
	// OnTicketReply notifies the ticket owner (nil = none).
	OnTicketReply func(ctx context.Context, t *domain.Ticket, body string)
}

type handlers struct{ Deps }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Register mounts POST /mcp.
func Register(mux *http.ServeMux, d Deps) {
	h := &handlers{d}
	mux.HandleFunc("POST /mcp", h.serve)
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, r *http.Request) {
		// No server-initiated stream; clients that probe GET get a clear answer.
		http.Error(w, "use POST with JSON-RPC", http.StatusMethodNotAllowed)
	})
}

func (h *handlers) serve(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	u := (*domain.User)(nil)
	if tok != "" && h.Resolve != nil {
		u = h.Resolve(r.Context(), tok)
	}
	if u == nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="captain"`)
		http.Error(w, "a staff API token is required", http.StatusUnauthorized)
		return
	}
	var req request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeRPC(w, response{JSONRPC: "2.0", Error: &rpcError{-32700, "parse error"}})
		return
	}
	// Notifications carry no id and get no body.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	res := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		res.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "captain", "version": h.Version},
			"instructions":    "Captain proxy-panel tools. Read tools list nodes, users, plans, orders, tickets and monitoring. Write tools (create/grant/adjust/reply) need confirm=true and an admin or operator token.",
		}
	case "ping":
		res.Result = map[string]any{}
	case "tools/list":
		res.Result = map[string]any{"tools": h.toolList(u)}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		out, err := h.call(r.Context(), u, p.Name, p.Arguments)
		if err != nil {
			res.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}
		} else {
			b, _ := json.MarshalIndent(out, "", "  ")
			res.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
		}
	default:
		res.Error = &rpcError{-32601, "method not found: " + req.Method}
	}
	writeRPC(w, res)
}

func writeRPC(w http.ResponseWriter, res response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// tool describes one MCP tool; write tools are hidden from support tokens.
type tool struct {
	Name, Description string
	Write, Ticket     bool
	Schema            map[string]any
}

func prop(t, desc string) map[string]any { return map[string]any{"type": t, "description": desc} }

func schema(required []string, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

var confirmProp = prop("boolean", "must be true to perform this write")

var tools = []tool{
	{Name: "dashboard", Description: "Panel overview: users, active subscriptions, nodes online, revenue today/month, pending orders, open tickets.", Schema: schema(nil, map[string]any{})},
	{Name: "node_list", Description: "Nodes with online state, version, last report, host metrics.", Schema: schema(nil, map[string]any{})},
	{Name: "node_detail", Description: "One node with its inbounds, live host metrics, probe data and monthly traffic.", Schema: schema([]string{"node_id"}, map[string]any{"node_id": prop("integer", "node id")})},
	{Name: "plan_list", Description: "Plans with prices and periods.", Schema: schema(nil, map[string]any{})},
	{Name: "user_list", Description: "Search users by email substring; paged.", Schema: schema(nil, map[string]any{"query": prop("string", "email substring"), "page": prop("integer", "1-based page")})},
	{Name: "user_detail", Description: "One user by id or email: subscription, balance, recent orders, online devices.", Schema: schema(nil, map[string]any{"user_id": prop("integer", "user id"), "email": prop("string", "email")})},
	{Name: "order_list", Description: "Recent orders, optionally by status (pending|paid|cancelled).", Schema: schema(nil, map[string]any{"status": prop("string", "status filter"), "limit": prop("integer", "max rows, default 50")})},
	{Name: "ticket_list", Description: "Support tickets, optionally by status (open|replied|closed).", Ticket: true, Schema: schema(nil, map[string]any{"status": prop("string", "status filter")})},
	{Name: "ticket_detail", Description: "A ticket with its full thread.", Ticket: true, Schema: schema([]string{"ticket_id"}, map[string]any{"ticket_id": prop("integer", "ticket id")})},
	{Name: "probe_snapshot", Description: "Live host metrics and latency of every node from the probe (when enabled).", Schema: schema(nil, map[string]any{})},
	{Name: "tcping", Description: "TCP-connect latency from the panel to host:port, three tries.", Schema: schema([]string{"host", "port"}, map[string]any{"host": prop("string", "hostname or IP"), "port": prop("integer", "port")})},
	{Name: "external_node_list", Description: "Imported external nodes and subscription sources.", Schema: schema(nil, map[string]any{})},
	{Name: "user_create", Description: "Create a user with a password.", Write: true, Schema: schema([]string{"email", "password", "confirm"}, map[string]any{"email": prop("string", "email"), "password": prop("string", "8+ chars"), "confirm": confirmProp})},
	{Name: "user_grant_plan", Description: "Give a user a plan now (replaces or renews the active one).", Write: true, Schema: schema([]string{"user_id", "plan_id", "confirm"}, map[string]any{"user_id": prop("integer", "user id"), "plan_id": prop("integer", "plan id"), "confirm": confirmProp})},
	{Name: "user_adjust", Description: "Extend days, override quota (GB, 0 = unlimited, -1 = plan default), set monthly reset day, reset usage.", Write: true, Schema: schema([]string{"user_id", "confirm"}, map[string]any{"user_id": prop("integer", "user id"), "add_days": prop("integer", "days to add"), "quota_gb": prop("number", "quota override in GB; 0 unlimited; -1 plan default"), "reset_day": prop("integer", "1-28, 0 = plan rule"), "reset_usage": prop("boolean", "zero the counters"), "confirm": confirmProp})},
	{Name: "user_balance", Description: "Add (or subtract) cents to a user's balance.", Write: true, Schema: schema([]string{"user_id", "delta_cents", "confirm"}, map[string]any{"user_id": prop("integer", "user id"), "delta_cents": prop("integer", "signed cents"), "confirm": confirmProp})},
	{Name: "user_set_status", Description: "Ban or re-activate a user.", Write: true, Schema: schema([]string{"user_id", "status", "confirm"}, map[string]any{"user_id": prop("integer", "user id"), "status": prop("string", "active|banned"), "confirm": confirmProp})},
	{Name: "ticket_reply", Description: "Reply to a ticket as staff (the user is notified).", Ticket: true, Write: true, Schema: schema([]string{"ticket_id", "body", "confirm"}, map[string]any{"ticket_id": prop("integer", "ticket id"), "body": prop("string", "reply text"), "confirm": confirmProp})},
}

func (h *handlers) toolList(u *domain.User) []map[string]any {
	out := []map[string]any{}
	for _, t := range tools {
		if !allowedTool(u, t) {
			continue
		}
		out = append(out, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
	}
	return out
}

// allowedTool mirrors the admin role matrix: support = tickets + reads of
// users/orders/plans/dashboard; operator = everything but no extra; admin = all.
func allowedTool(u *domain.User, t tool) bool {
	switch u.Role {
	case domain.RoleAdmin, domain.RoleOperator:
		return true
	case domain.RoleSupport:
		if t.Ticket {
			return true
		}
		return !t.Write && (t.Name == "dashboard" || t.Name == "user_list" || t.Name == "user_detail" || t.Name == "order_list" || t.Name == "plan_list")
	}
	return false
}

func argInt(a map[string]any, k string) int64 {
	switch v := a[k].(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

func argStr(a map[string]any, k string) string {
	s, _ := a[k].(string)
	return strings.TrimSpace(s)
}

func argBool(a map[string]any, k string) bool {
	b, _ := a[k].(bool)
	return b
}

func (h *handlers) call(ctx context.Context, u *domain.User, name string, a map[string]any) (any, error) {
	var t *tool
	for i := range tools {
		if tools[i].Name == name {
			t = &tools[i]
		}
	}
	if t == nil || !allowedTool(u, *t) {
		return nil, fmt.Errorf("unknown or not permitted tool %q", name)
	}
	if a == nil {
		a = map[string]any{}
	}
	if t.Write && !argBool(a, "confirm") {
		return nil, errors.New("this tool changes data: call again with confirm=true")
	}
	now := time.Now()
	st := h.Store
	switch name {
	case "dashboard":
		stats, err := st.Dashboard(ctx, now)
		if err != nil {
			return nil, err
		}
		open, _ := st.OpenTickets(ctx)
		return map[string]any{"stats": stats, "open_tickets": open}, nil
	case "node_list":
		nodes, err := st.ListNodes(ctx)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, n := range nodes {
			row := map[string]any{"id": n.ID, "name": n.Name, "public_addr": n.PublicAddr, "version": n.Version, "paired": n.Paired, "last_seen_at": n.LastSeenAt, "online": n.LastSeenAt != nil && now.Sub(*n.LastSeenAt) < 3*time.Minute}
			if h.Probe != nil {
				if l, ok := h.Probe.Live(n.ID); ok {
					row["host"] = l.Host
				}
			}
			out = append(out, row)
		}
		return out, nil
	case "node_detail":
		id := argInt(a, "node_id")
		n, err := st.NodeByID(ctx, id)
		if err != nil {
			return nil, err
		}
		inbounds, _ := st.AllInboundsByNode(ctx, id)
		status, _ := st.NodeStatus(ctx, id)
		np, _ := st.NodeProbe(ctx, id)
		out := map[string]any{"node": n, "inbounds": inbounds, "status": status, "probe": np}
		if h.Probe != nil {
			if l, ok := h.Probe.Live(id); ok {
				out["live"] = l
			}
		}
		return out, nil
	case "plan_list":
		return st.ListPlans(ctx, false)
	case "user_list":
		page := int(argInt(a, "page"))
		if page < 1 {
			page = 1
		}
		rows, total, err := st.ListUsers(ctx, argStr(a, "query"), 50, (page-1)*50, now)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, r := range rows {
			out = append(out, map[string]any{"id": r.User.ID, "email": r.User.Email, "status": r.User.Status, "balance_cents": r.User.BalanceCents, "plan": r.PlanName, "expires_at": r.ExpiresAt, "quota_bytes": r.QuotaBytes, "used_bytes": r.UsedBytes, "usable": r.SubUsable})
		}
		return map[string]any{"total": total, "page": page, "users": out}, nil
	case "user_detail":
		var user *domain.User
		var err error
		if e := argStr(a, "email"); e != "" {
			user, err = st.UserByEmail(ctx, strings.ToLower(e))
		} else {
			user, err = st.UserByID(ctx, argInt(a, "user_id"))
		}
		if err != nil {
			return nil, err
		}
		sub, _ := st.ActiveSubscription(ctx, user.ID)
		subs, _ := st.Subscriptions(ctx, user.ID)
		orders, _ := st.OrdersByUser(ctx, user.ID, 10)
		devices, _ := st.OnlineDevices(ctx, user.ID, now.Add(-5*time.Minute))
		return map[string]any{"id": user.ID, "email": user.Email, "status": user.Status, "role": user.Role, "balance_cents": user.BalanceCents, "group_id": user.GroupID, "created_at": user.CreatedAt, "subscription": sub, "subscriptions": subs, "orders": orders, "online_devices": devices}, nil
	case "order_list":
		limit := int(argInt(a, "limit"))
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		rows, total, err := st.ListOrders(ctx, argStr(a, "status"), limit, 0)
		if err != nil {
			return nil, err
		}
		return map[string]any{"total": total, "orders": rows}, nil
	case "ticket_list":
		rows, total, err := st.ListTickets(ctx, 0, argStr(a, "status"), 100, 0)
		if err != nil {
			return nil, err
		}
		return map[string]any{"total": total, "tickets": rows}, nil
	case "ticket_detail":
		tk, err := st.TicketByID(ctx, argInt(a, "ticket_id"))
		if err != nil {
			return nil, err
		}
		msgs, _ := st.TicketMessages(ctx, tk.ID)
		return map[string]any{"ticket": tk, "messages": msgs}, nil
	case "probe_snapshot":
		if h.Probe == nil || !h.Probe.Settings(ctx).Enabled {
			return nil, errors.New("the probe is off (Settings → Probe)")
		}
		nodes, _ := st.ListNodes(ctx)
		out := []map[string]any{}
		for _, n := range nodes {
			if l, ok := h.Probe.Live(n.ID); ok {
				out = append(out, map[string]any{"id": n.ID, "name": n.Name, "at": l.At, "host": l.Host})
			}
		}
		return out, nil
	case "tcping":
		host, port := argStr(a, "host"), int(argInt(a, "port"))
		if host == "" || port <= 0 {
			return nil, errors.New("host and port are required")
		}
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		best := -1.0
		for i := 0; i < 3; i++ {
			dctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			start := time.Now()
			c, err := (&net.Dialer{}).DialContext(dctx, "tcp", addr)
			ms := float64(time.Since(start).Microseconds()) / 1000
			cancel()
			if err == nil {
				c.Close()
			} else if ne, ok := err.(net.Error); !ok || ne.Timeout() {
				continue
			}
			if best < 0 || ms < best {
				best = ms
			}
		}
		return map[string]any{"host": host, "port": port, "min_ms": best, "reachable": best >= 0}, nil
	case "external_node_list":
		srcs, _ := st.ListExternalSources(ctx)
		nodes, _ := st.ListExternalNodes(ctx)
		return map[string]any{"sources": srcs, "nodes": nodes}, nil
	case "user_create":
		email, pw := strings.ToLower(argStr(a, "email")), argStr(a, "password")
		if !strings.Contains(email, "@") || len(pw) < 8 {
			return nil, errors.New("valid email and a password of 8+ chars are required")
		}
		if h.NewUser == nil {
			return nil, errors.New("user creation not available")
		}
		nu, err := h.NewUser(email, pw)
		if err != nil {
			return nil, err
		}
		if err := st.CreateUser(ctx, nu); err != nil {
			return nil, errors.New("email already registered")
		}
		return map[string]any{"id": nu.ID, "email": nu.Email}, nil
	case "user_grant_plan":
		plan, err := st.PlanByID(ctx, argInt(a, "plan_id"))
		if err != nil {
			return nil, errors.New("unknown plan")
		}
		return st.GrantSubscription(ctx, argInt(a, "user_id"), plan, now)
	case "user_adjust":
		adj := store.SubAdjust{AddDays: int(argInt(a, "add_days")), ResetUsage: argBool(a, "reset_usage")}
		if v, ok := a["quota_gb"].(float64); ok {
			var q int64
			switch {
			case v < 0:
				q = 0
			case v == 0:
				q = -1
			default:
				q = int64(v * (1 << 30))
			}
			adj.QuotaOverride = &q
		}
		if v, ok := a["reset_day"].(float64); ok {
			d := int(v)
			adj.ResetDay = &d
		}
		return st.AdjustSubscription(ctx, argInt(a, "user_id"), adj, now)
	case "user_balance":
		id, delta := argInt(a, "user_id"), argInt(a, "delta_cents")
		if err := st.AdjustBalance(ctx, id, delta); err != nil {
			return nil, err
		}
		user, err := st.UserByID(ctx, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": user.ID, "balance_cents": user.BalanceCents}, nil
	case "user_set_status":
		status := argStr(a, "status")
		if status != "active" && status != "banned" {
			return nil, errors.New("status must be active or banned")
		}
		user, err := st.UserByID(ctx, argInt(a, "user_id"))
		if err != nil {
			return nil, err
		}
		if err := st.UpdateUser(ctx, user.ID, status, user.GroupID, ""); err != nil {
			return nil, err
		}
		return map[string]any{"id": user.ID, "status": status}, nil
	case "ticket_reply":
		tk, err := st.TicketByID(ctx, argInt(a, "ticket_id"))
		if err != nil {
			return nil, err
		}
		body := argStr(a, "body")
		if body == "" {
			return nil, errors.New("body is required")
		}
		if err := st.ReplyTicket(ctx, tk.ID, true, body); err != nil {
			return nil, err
		}
		if h.OnTicketReply != nil {
			h.OnTicketReply(ctx, tk, body)
		}
		return map[string]any{"ticket_id": tk.ID, "status": store.TicketReplied}, nil
	}
	return nil, fmt.Errorf("tool %q has no implementation", name)
}
