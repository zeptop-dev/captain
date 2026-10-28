package komari

import (
	"encoding/json"
	"sort"
	"strings"
)

// The upstream RPC framework exposes these four introspection/health methods.
// Keep discovery limited to this transport's supported public methods; importing
// an upstream registry could advertise or accidentally expose administration.
var callableMethods = []string{
	"common:getMe", "common:getNodeRecentStatus", "common:getNodes", "common:getNodesLatestStatus", "common:getPublicInfo", "common:getRecords", "common:getVersion",
	"public:getClientRecentRecords", "public:getMe", "public:getNodesInformation", "public:getPingMetricStats", "public:getPingRecords", "public:getPublicPingTasks", "public:getPublicSettings", "public:getRecordsByUUID", "public:getVersion", "public:listMetricDefinitions", "public:queryMetrics",
	"rpc.help", "rpc.methods", "rpc.ping", "rpc.version",
}

type methodHelp struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Returns string `json:"returns,omitempty"`
}

func helpFor(name string) methodHelp {
	h := methodHelp{Name: name}
	switch name {
	case "rpc.ping":
		h.Summary, h.Returns = "Health check", "string"
	case "rpc.version":
		h.Summary, h.Returns = "RPC protocol version", "string"
	case "rpc.methods":
		h.Summary, h.Returns = "List supported read-only methods", "[]string"
	case "rpc.help":
		h.Summary, h.Returns = "Describe supported read-only methods", "MethodMeta"
	}
	return h
}

func frameworkCall(method string, raw json.RawMessage) (any, *rpcError) {
	switch method {
	case "rpc.ping":
		return "pong", nil
	case "rpc.version":
		return "2.0", nil
	case "rpc.methods":
		var p struct {
			Internal bool `json:"internal"`
		}
		if bind(raw, &p) != nil {
			return nil, invalid("Invalid method selector")
		}
		names := make([]string, 0, len(callableMethods))
		for _, name := range callableMethods {
			if p.Internal || !strings.HasPrefix(name, "rpc.") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		return names, nil
	default: // rpc.help
		var p struct {
			Method string `json:"method"`
		}
		if bind(raw, &p) != nil || len(p.Method) > 128 {
			return nil, invalid("Invalid method selector")
		}
		if p.Method != "" {
			for _, name := range callableMethods {
				if p.Method == name {
					return helpFor(name), nil
				}
			}
			return nil, &rpcError{Code: -32600, Message: "method not found"}
		}
		out := make([]methodHelp, 0, len(callableMethods))
		for _, name := range callableMethods {
			h := helpFor(name)
			h.Returns = "" // Upstream's overview contains name and summary only.
			out = append(out, h)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
		return out, nil
	}
}
