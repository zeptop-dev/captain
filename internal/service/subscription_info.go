package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/store"
	"gopkg.in/yaml.v3"
)

// WithAccountInfo runs after normal template expansion. Information is expressed
// as isolated selectors, not proxy endpoints: wildcard proxy/provider selection,
// regional groups and automatic tests cannot accidentally acquire fake servers.
// Native formats without these selectors retain only Subscription-Userinfo.
func (s *Subscription) WithAccountInfo(ctx context.Context, u *domain.User, at time.Time, format string, body []byte, settings store.AccountInfoSettings) ([]byte, error) {
	if !settings.Enabled() || (format != "clash" && format != "stash") {
		return body, nil
	}
	if u.Role != domain.RoleUser || u.Status != "active" {
		return body, ErrDisabled
	}
	subs, err := s.Store.ActiveSubscriptions(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, sub := range subs {
		if sub.ExpiresAt != nil && !at.Before(*sub.ExpiresAt) {
			continue
		}
		if _, ok := names[sub.PlanID]; !ok {
			plan, err := s.Store.PlanByID(ctx, sub.PlanID)
			if err != nil {
				return nil, err
			}
			names[sub.PlanID] = plan.Name
		}
	}
	return addAccountInfoGroups(body, accountInfoLabels(subs, names, at, settings))
}

func accountInfoLabels(subs []*domain.Subscription, plans map[int64]string, at time.Time, settings store.AccountInfoSettings) []string {
	var labels []string
	for _, sub := range subs {
		if sub.Status != "active" || (sub.ExpiresAt != nil && !at.Before(*sub.ExpiresAt)) {
			continue
		}
		prefix := strings.TrimSpace(plans[sub.PlanID])
		if prefix == "" {
			prefix = "Plan"
		}
		prefix += " · "
		if settings.Traffic {
			limit := "Unlimited"
			if sub.QuotaBytes > 0 {
				limit = accountInfoBytes(sub.QuotaBytes)
			}
			labels = append(labels, prefix+"Traffic: "+accountInfoBytes(sub.UsedUpBytes+sub.UsedDownBytes)+" / "+limit)
		}
		if settings.Reset {
			reset := "No scheduled reset"
			if sub.ResetAt != nil {
				switch {
				case sub.ExpiresAt != nil && !sub.ResetAt.Before(*sub.ExpiresAt):
					reset = "No reset before expiry"
				case !sub.ResetAt.After(at):
					reset = "Pending"
				default:
					// Include the stored instant rather than inventing a cadence.
					days := int(sub.ResetAt.Sub(at).Hours() / 24)
					reset = fmt.Sprintf("%s UTC (%d days left)", sub.ResetAt.UTC().Format("2006-01-02 15:04"), days)
				}
			}
			labels = append(labels, prefix+"Traffic reset: "+reset)
		}
		if settings.Expiry {
			expiry := "Never"
			if sub.ExpiresAt != nil {
				expiry = sub.ExpiresAt.UTC().Format("2006-01-02 15:04") + " UTC"
			}
			labels = append(labels, prefix+"Expires: "+expiry)
		}
	}
	if len(labels) == 0 {
		labels = append(labels, "No active subscription")
	}
	return labels
}

func addAccountInfoGroups(body []byte, labels []string) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("subscription document must be a mapping")
	}
	root := doc.Content[0]
	used := map[string]bool{"DIRECT": true, "REJECT": true, "GLOBAL": true}
	var groups *yaml.Node
	// Reserve even names outside the proxy catalogue. Do not let a generated
	// label satisfy a previously unresolved rule or manual reference.
	var reserve func(*yaml.Node)
	reserve = func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode {
			used[n.Value] = true
			for _, part := range strings.Split(n.Value, ",") {
				used[strings.TrimSpace(part)] = true
			}
		}
		for _, child := range n.Content {
			reserve(child)
		}
	}
	reserve(root)
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == "proxy-groups" {
			groups = root.Content[i+1]
			break
		}
	}
	if groups == nil {
		groups = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "proxy-groups"}, groups)
	}
	if groups.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("subscription proxy-groups must be a sequence")
	}
	unique := func(label string) string {
		name := label
		for suffix := 2; used[name]; suffix++ {
			name = fmt.Sprintf("%s (%d)", label, suffix)
		}
		used[name] = true
		return name
	}
	group := func(name string, members []string) *yaml.Node {
		var node yaml.Node
		_ = node.Encode(struct {
			Name    string   `yaml:"name"`
			Type    string   `yaml:"type"`
			Proxies []string `yaml:"proxies"`
		}{name, "select", members})
		return &node
	}
	parent := unique("Subscription info")
	var members []string
	var children []*yaml.Node
	for _, label := range labels {
		name := unique(label)
		members = append(members, name)
		children = append(children, group(name, []string{"REJECT"}))
	}
	// Existing groups and rules retain their order, membership and defaults.
	groups.Content = append(groups.Content, group(parent, members))
	groups.Content = append(groups.Content, children...)
	return yaml.Marshal(&doc)
}

// Explicit binary units match the quota byte counters without rounding to whole GB.
func accountInfoBytes(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	value, unit := float64(n), 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if unit == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.2f %s", value, units[unit])
}
