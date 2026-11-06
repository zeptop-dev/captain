package store

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zeptop-dev/bosun/pkg/agentproto"
	"github.com/zeptop-dev/captain/internal/domain"
)

// Observation and published entry addresses have separate lifetimes. Never
// infer a NAT entry address from the IP used by an outbound HTTP request.
type AddressFamily struct {
	Address        string `json:"address"`
	LastSuccess    int64  `json:"last_success"`
	CandidateSince int64  `json:"candidate_since"`
	ConfirmedAt    int64  `json:"confirmed_at"`
}

// AddressFallback is advisory single-source evidence. It never enters the
// confirmation or reconciliation fields consumed by older Captain versions.
type AddressFallback struct {
	agentproto.PublicAddressFallback
	ReceivedAt int64 `json:"received_at"`
}

// FallbackFollow is separate from the consensus fields read by older panels.
// Both sample and receipt clocks must advance; a retried report is not evidence.
type FallbackFollow struct {
	Address            string `json:"address"`
	Source             string `json:"source"`
	Origin             string `json:"origin"`
	RunID              string `json:"run_id"`
	CandidateSince     int64  `json:"candidate_since"`
	CandidateSampledAt int64  `json:"candidate_sampled_at"`
	LastSampledAt      int64  `json:"last_sampled_at"`
	LastSuccess        int64  `json:"last_success"`
	ConfirmedAt        int64  `json:"confirmed_at"`
}
type AddressObservation struct {
	FallbackFollowIPv4 FallbackFollow   `json:"fallback_follow_ipv4"`
	FallbackFollowIPv6 FallbackFollow   `json:"fallback_follow_ipv6"`
	FallbackIPv4       *AddressFallback `json:"fallback_ipv4,omitempty"`
	FallbackIPv6       *AddressFallback `json:"fallback_ipv6,omitempty"`
	SampledAt          int64            `json:"sampled_at"`
	ReceivedAt         int64            `json:"received_at"`
	IPv4               AddressFamily    `json:"ipv4"`
	IPv6               AddressFamily    `json:"ipv6"`
}
type NodeAddresses struct {
	FallbackAutoIPv4 bool               `json:"fallback_auto_ipv4"`
	FallbackAutoIPv6 bool               `json:"fallback_auto_ipv6"`
	AutoIPv4         bool               `json:"auto_ipv4"`
	AutoIPv6         bool               `json:"auto_ipv6"`
	EntryIPv4        string             `json:"entry_ipv4"`
	EntryIPv6        string             `json:"entry_ipv6"`
	Observed         AddressObservation `json:"observed"`
	DNSPending       bool               `json:"dns_pending"`
}

func (s *Store) NodeAddresses(ctx context.Context, id int64) (NodeAddresses, error) {
	var out NodeAddresses
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT address_auto_v4,address_auto_v6,address_fallback_auto_v4,address_fallback_auto_v6,public_addr,v6_addr,address_json,address_dns_pending FROM nodes WHERE id=?`, id).Scan(&out.AutoIPv4, &out.AutoIPv6, &out.FallbackAutoIPv4, &out.FallbackAutoIPv6, &out.EntryIPv4, &out.EntryIPv6, &raw, &out.DNSPending)
	if err != nil {
		return out, wrapNotFound(err)
	}
	err = json.Unmarshal([]byte(raw), &out.Observed)
	return out, err
}

// SetAddressPolicy requires Topology. Existing nodes default to manual; an old
// client that edits unrelated fields cannot enable automatic following.
func (s *Store) SetAddressPolicy(ctx context.Context, id int64, v4, v6 bool) error {
	return s.SetAddressFollowPolicy(ctx, id, v4, v6, nil, nil)
}

// Nil fallback choices preserve the persisted policy for older API clients.
func (s *Store) SetAddressFollowPolicy(ctx context.Context, id int64, v4, v6 bool, fallback4, fallback6 *bool) error {
	return s.setAddressFollowPolicyAt(ctx, id, v4, v6, fallback4, fallback6, time.Now())
}

func (s *Store) setAddressFollowPolicyAt(ctx context.Context, id int64, v4, v6 bool, fallback4, fallback6 *bool, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old4, old6, f4, f6 bool
	var boundary4, boundary6 int64
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT address_auto_v4,address_auto_v6,address_fallback_auto_v4,address_fallback_auto_v6,address_fallback_changed_v4,address_fallback_changed_v6,address_json FROM nodes WHERE id=?`, id).Scan(&old4, &old6, &f4, &f6, &boundary4, &boundary6, &raw)
	if err != nil {
		return wrapNotFound(err)
	}
	var obs AddressObservation
	if err = json.Unmarshal([]byte(raw), &obs); err != nil {
		return err
	}
	changed4, changed6 := old4 != v4, old6 != v6
	if fallback4 != nil {
		changed4 = changed4 || f4 != *fallback4
		f4 = *fallback4
	}
	if fallback6 != nil {
		changed6 = changed6 || f6 != *fallback6
		f6 = *fallback6
	}
	// Reject already observed samples even when the node clock is ahead.
	boundary := max(at.Unix(), obs.SampledAt)
	if changed4 {
		boundary4 = max(boundary4, boundary)
		obs.FallbackFollowIPv4 = FallbackFollow{}
	}
	if changed6 {
		boundary6 = max(boundary6, boundary)
		obs.FallbackFollowIPv6 = FallbackFollow{}
	}
	buf, err := json.Marshal(obs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET address_auto_v4=?,address_auto_v6=?,address_fallback_auto_v4=?,address_fallback_auto_v6=?,address_fallback_changed_v4=?,address_fallback_changed_v6=?,address_json=?,updated_at=? WHERE id=?`, boolInt(v4), boolInt(v6), boolInt(f4), boolInt(f6), boundary4, boundary6, string(buf), at.Unix(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ObserveNodeAddresses is token-bound and transactionally deduplicated. Cached
// reports, re-pairing and an old report racing a newer sample cannot promote a
// candidate. Unknown families retain the last known value but lose confirmation.
func (s *Store) ObserveNodeAddresses(ctx context.Context, id int64, token string, sample *agentproto.PublicAddresses, at time.Time) error {
	if sample == nil {
		return nil
	}
	freshSample := sample.SampledAt >= at.Add(-15*time.Minute).Unix() && sample.SampledAt <= at.Add(time.Minute).Unix()
	if freshSample && (sample.IPv4 != "" && !agentproto.ValidPublicAddress(sample.IPv4, false) || sample.IPv6 != "" && !agentproto.ValidPublicAddress(sample.IPv6, true)) {
		return errors.New("invalid public address observation")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw, entry4, entry6 string
	var auto4, auto6, fallback4, fallback6 bool
	var boundary4, boundary6 int64
	if err = tx.QueryRowContext(ctx, `SELECT address_json,public_addr,v6_addr,address_auto_v4,address_auto_v6,address_fallback_auto_v4,address_fallback_auto_v6,address_fallback_changed_v4,address_fallback_changed_v6 FROM nodes WHERE id=? AND token_hash=?`, id, token).Scan(&raw, &entry4, &entry6, &auto4, &auto6, &fallback4, &fallback6, &boundary4, &boundary6); err != nil {
		return wrapNotFound(err)
	}
	var obs AddressObservation
	if err = json.Unmarshal([]byte(raw), &obs); err != nil {
		return err
	}
	if sample.SampledAt <= obs.SampledAt {
		return nil
	}
	if !freshSample {
		// A newer invalid sample breaks the fallback run, but cannot move the
		// deduplication cursor into the future or alter legacy consensus data.
		obs.FallbackFollowIPv4, obs.FallbackFollowIPv6 = FallbackFollow{}, FallbackFollow{}
		buf, _ := json.Marshal(obs)
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET address_json=? WHERE id=? AND token_hash=?`, string(buf), id, token); err != nil {
			return err
		}
		return tx.Commit()
	}
	advance := func(f *AddressFamily, value, entry string) {
		if value != "" {
			value = netip.MustParseAddr(value).String()
		}
		if value == "" {
			f.CandidateSince = 0
			f.ConfirmedAt = 0
			return
		}
		if f.Address != value || f.CandidateSince == 0 || at.Unix()-f.LastSuccess > 900 {
			f.CandidateSince = at.Unix()
			f.ConfirmedAt = 0
		}
		confirmationSeconds := int64(120)
		if entry == "" {
			confirmationSeconds = 30
		}
		if at.Unix()-f.CandidateSince >= confirmationSeconds {
			f.ConfirmedAt = at.Unix()
		}
		f.Address = value
		f.LastSuccess = at.Unix()
	}
	advance(&obs.IPv4, sample.IPv4, entry4)
	advance(&obs.IPv6, sample.IPv6, entry6)
	obs.FallbackIPv4 = addressFallback(sample.FallbackIPv4, sample.SampledAt, at, false)
	obs.FallbackIPv6 = addressFallback(sample.FallbackIPv6, sample.SampledAt, at, true)
	advanceFallback(&obs.FallbackFollowIPv4, obs.FallbackIPv4, auto4 && fallback4 && sample.IPv4 == "", boundary4, at)
	advanceFallback(&obs.FallbackFollowIPv6, obs.FallbackIPv6, auto6 && fallback6 && sample.IPv6 == "", boundary6, at)
	obs.SampledAt = sample.SampledAt
	obs.ReceivedAt = at.Unix()
	buf, _ := json.Marshal(obs)
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET address_json=? WHERE id=? AND token_hash=?`, string(buf), id, token)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ReconcileNodeAddresses requires Topology. Only confirmed, fresh observations
// may change an opted-in family. Explicit entry/ingress/WG endpoints are untouched.
func (s *Store) ReconcileNodeAddresses(ctx context.Context, id int64, at time.Time) (*domain.Node, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	var raw, ip4, ip6 string
	var auto4, auto6, fallback4, fallback6, pending bool
	var attempt int64
	err = tx.QueryRowContext(ctx, `SELECT address_auto_v4,address_auto_v6,address_fallback_auto_v4,address_fallback_auto_v6,public_addr,v6_addr,address_json,address_dns_pending,address_dns_attempt FROM nodes WHERE id=? AND token_hash IS NOT NULL`, id).Scan(&auto4, &auto6, &fallback4, &fallback6, &ip4, &ip6, &raw, &pending, &attempt)
	if err != nil {
		return nil, false, wrapNotFound(err)
	}
	var obs AddressObservation
	if err = json.Unmarshal([]byte(raw), &obs); err != nil {
		return nil, false, err
	}
	cleared := false
	clearExpired := func(f *FallbackFollow, sample *AddressFallback, enabled bool) {
		if f.CandidateSince != 0 && (!enabled || !fallbackFollowCurrent(*f, sample, at)) {
			*f = FallbackFollow{}
			cleared = true
		}
	}
	clearExpired(&obs.FallbackFollowIPv4, obs.FallbackIPv4, auto4 && fallback4)
	clearExpired(&obs.FallbackFollowIPv6, obs.FallbackIPv6, auto6 && fallback6)
	if cleared {
		buf, _ := json.Marshal(obs)
		if _, err = tx.ExecContext(ctx, `UPDATE nodes SET address_json=? WHERE id=?`, string(buf), id); err != nil {
			return nil, false, err
		}
	}
	old4, old6 := ip4, ip6
	valid := func(f AddressFamily) bool {
		return f.ConfirmedAt > 0 && f.LastSuccess >= at.Add(-15*time.Minute).Unix()
	}
	if auto4 && valid(obs.IPv4) {
		ip4 = obs.IPv4.Address
	} else if auto4 && fallback4 && validFallbackFollow(obs.FallbackFollowIPv4, obs.FallbackIPv4, at) {
		ip4 = obs.FallbackFollowIPv4.Address
	}
	if auto6 && valid(obs.IPv6) {
		ip6 = obs.IPv6.Address
	} else if auto6 && fallback6 && validFallbackFollow(obs.FallbackFollowIPv6, obs.FallbackIPv6, at) {
		ip6 = obs.FallbackFollowIPv6.Address
	}
	changed := old4 != ip4 || old6 != ip6
	if changed {
		pending = true
		attempt = 0
	}
	if changed {
		_, err = tx.ExecContext(ctx, `UPDATE nodes SET public_addr=?,v6_addr=?,address_dns_pending=?,address_dns_attempt=?,updated_at=? WHERE id=?`, ip4, ip6, boolInt(pending), attempt, at.Unix(), id)
		if err != nil {
			return nil, false, err
		}
	}
	if changed {
		if err = refreshLinesForNodeTx(ctx, tx, id); err != nil {
			return nil, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	n, err := s.NodeByID(ctx, id)
	return n, pending && at.Unix()-attempt >= 300, err
}
func (s *Store) BeginAddressDNS(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET address_dns_attempt=? WHERE id=?`, at.Unix(), id)
	return err
}
func (s *Store) FinishAddressDNS(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE nodes SET address_dns_pending=0 WHERE id=?`, id)
	return err
}

// Invalid optional evidence must not prevent otherwise valid observations from
// being recorded. Only bounded, credential-free origin and error metadata is kept.
func addressFallback(in *agentproto.PublicAddressFallback, sampledAt int64, at time.Time, v6 bool) *AddressFallback {
	if in == nil || in.Source != "captain_cloudflare_trace" || len(in.Origin) > 2048 || in.SampledAt <= 0 || in.SampledAt > sampledAt+1 || in.SampledAt < sampledAt-60 {
		return nil
	}
	u, err := url.Parse(in.Origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(u.Host, ":") {
		return nil
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil
		}
	}
	value := *in
	// Unknown optional proof is still displayable, but can never authorize following.
	if value.CheckStatus != agentproto.PublicAddressFallbackCheckNoConflict && value.CheckStatus != agentproto.PublicAddressFallbackCheckConflict {
		value.CheckStatus = ""
	}
	if !validFallbackRunID(value.RunID) {
		value.RunID = ""
	}
	if value.Address != "" {
		if value.Error != "" || !agentproto.ValidPublicAddress(value.Address, v6) {
			return nil
		}
		value.Address = netip.MustParseAddr(value.Address).String()
	} else {
		switch value.Error {
		case "timeout", "network", "tls", "http_status", "invalid_response", "invalid_origin":
		default:
			return nil
		}
	}
	return &AddressFallback{PublicAddressFallback: value, ReceivedAt: at.Unix()}
}

func validFallbackRunID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func advanceFallback(f *FallbackFollow, sample *AddressFallback, enabled bool, boundary int64, at time.Time) {
	if !enabled || sample == nil || sample.Address == "" || sample.Error != "" ||
		sample.CheckStatus != agentproto.PublicAddressFallbackCheckNoConflict ||
		!validFallbackRunID(sample.RunID) || sample.SampledAt <= boundary {
		*f = FallbackFollow{}
		return
	}
	sameRun := f.Address == sample.Address && f.Source == sample.Source && f.Origin == sample.Origin && f.RunID == sample.RunID
	continuous := sample.SampledAt > f.LastSampledAt && sample.SampledAt-f.LastSampledAt <= 900 &&
		at.Unix() >= f.LastSuccess && at.Unix()-f.LastSuccess <= 900
	if !sameRun || !continuous || f.CandidateSince == 0 {
		*f = FallbackFollow{Address: sample.Address, Source: sample.Source, Origin: sample.Origin, RunID: sample.RunID, CandidateSince: at.Unix(), CandidateSampledAt: sample.SampledAt}
	}
	if f.LastSampledAt != 0 && sample.SampledAt-f.CandidateSampledAt >= 120 && at.Unix()-f.CandidateSince >= 120 {
		f.ConfirmedAt = at.Unix()
	}
	f.LastSuccess = at.Unix()
	f.LastSampledAt = sample.SampledAt
}

func validFallbackFollow(f FallbackFollow, sample *AddressFallback, at time.Time) bool {
	return f.ConfirmedAt > 0 && f.ConfirmedAt <= at.Unix() && fallbackFollowCurrent(f, sample, at)
}

func fallbackFollowCurrent(f FallbackFollow, sample *AddressFallback, at time.Time) bool {
	if sample == nil || sample.Error != "" || sample.CheckStatus != agentproto.PublicAddressFallbackCheckNoConflict || !validFallbackRunID(f.RunID) {
		return false
	}
	fresh := f.LastSuccess <= at.Unix() && f.LastSuccess >= at.Add(-15*time.Minute).Unix() &&
		f.LastSampledAt >= at.Add(-15*time.Minute).Unix() && f.LastSampledAt <= at.Add(time.Minute).Unix()
	return fresh && f.LastSuccess == sample.ReceivedAt && f.LastSampledAt == sample.SampledAt &&
		f.Address == sample.Address && f.Source == sample.Source && f.Origin == sample.Origin && f.RunID == sample.RunID
}
