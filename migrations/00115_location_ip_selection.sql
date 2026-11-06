-- +goose Up
ALTER TABLE nodes ADD COLUMN location_ip_json TEXT NOT NULL DEFAULT '{"mode":"auto","manual_ip":"","allow_entry_fallback":false}';
ALTER TABLE nodes ADD COLUMN location_ip_revision INTEGER NOT NULL DEFAULT 0;

-- A selection revision prevents late lookups accepting A→B→A. Successful
-- samples for the same identity do not increment it; freshness is rechecked
-- separately at commit. All writers, including pairing and entry updates,
-- participate without putting location metadata in the public probe profile.
-- +goose StatementBegin
CREATE TRIGGER nodes_location_ip_revision AFTER UPDATE OF location_ip_json,token_hash,public_addr,v6_addr,address_auto_v4,address_auto_v6,address_fallback_auto_v4,address_fallback_auto_v6,address_json ON nodes
WHEN OLD.location_ip_json IS NOT NEW.location_ip_json
 OR OLD.token_hash IS NOT NEW.token_hash
 OR OLD.public_addr IS NOT NEW.public_addr
 OR OLD.v6_addr IS NOT NEW.v6_addr
 OR OLD.address_auto_v4 IS NOT NEW.address_auto_v4
 OR OLD.address_auto_v6 IS NOT NEW.address_auto_v6
 OR OLD.address_fallback_auto_v4 IS NOT NEW.address_fallback_auto_v4
 OR OLD.address_fallback_auto_v6 IS NOT NEW.address_fallback_auto_v6
 OR json_extract(OLD.address_json,'$.ipv4.address') IS NOT json_extract(NEW.address_json,'$.ipv4.address')
 OR json_extract(OLD.address_json,'$.ipv4.candidate_since') IS NOT json_extract(NEW.address_json,'$.ipv4.candidate_since')
 OR json_extract(OLD.address_json,'$.ipv6.address') IS NOT json_extract(NEW.address_json,'$.ipv6.address')
 OR json_extract(OLD.address_json,'$.ipv6.candidate_since') IS NOT json_extract(NEW.address_json,'$.ipv6.candidate_since')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv4.address') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv4.address')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv4.source') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv4.source')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv4.origin') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv4.origin')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv4.run_id') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv4.run_id')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv4.candidate_since') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv4.candidate_since')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv4.candidate_sampled_at') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv4.candidate_sampled_at')
 OR (COALESCE(json_extract(OLD.address_json,'$.fallback_follow_ipv4.confirmed_at'),0)>0) IS NOT (COALESCE(json_extract(NEW.address_json,'$.fallback_follow_ipv4.confirmed_at'),0)>0)
 OR json_extract(OLD.address_json,'$.fallback_ipv4.address') IS NOT json_extract(NEW.address_json,'$.fallback_ipv4.address')
 OR json_extract(OLD.address_json,'$.fallback_ipv4.source') IS NOT json_extract(NEW.address_json,'$.fallback_ipv4.source')
 OR json_extract(OLD.address_json,'$.fallback_ipv4.origin') IS NOT json_extract(NEW.address_json,'$.fallback_ipv4.origin')
 OR json_extract(OLD.address_json,'$.fallback_ipv4.run_id') IS NOT json_extract(NEW.address_json,'$.fallback_ipv4.run_id')
 OR json_extract(OLD.address_json,'$.fallback_ipv4.check_status') IS NOT json_extract(NEW.address_json,'$.fallback_ipv4.check_status')
 OR json_extract(OLD.address_json,'$.fallback_ipv4.error') IS NOT json_extract(NEW.address_json,'$.fallback_ipv4.error')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv6.address') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv6.address')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv6.source') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv6.source')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv6.origin') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv6.origin')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv6.run_id') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv6.run_id')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv6.candidate_since') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv6.candidate_since')
 OR json_extract(OLD.address_json,'$.fallback_follow_ipv6.candidate_sampled_at') IS NOT json_extract(NEW.address_json,'$.fallback_follow_ipv6.candidate_sampled_at')
 OR (COALESCE(json_extract(OLD.address_json,'$.fallback_follow_ipv6.confirmed_at'),0)>0) IS NOT (COALESCE(json_extract(NEW.address_json,'$.fallback_follow_ipv6.confirmed_at'),0)>0)
 OR json_extract(OLD.address_json,'$.fallback_ipv6.address') IS NOT json_extract(NEW.address_json,'$.fallback_ipv6.address')
 OR json_extract(OLD.address_json,'$.fallback_ipv6.source') IS NOT json_extract(NEW.address_json,'$.fallback_ipv6.source')
 OR json_extract(OLD.address_json,'$.fallback_ipv6.origin') IS NOT json_extract(NEW.address_json,'$.fallback_ipv6.origin')
 OR json_extract(OLD.address_json,'$.fallback_ipv6.run_id') IS NOT json_extract(NEW.address_json,'$.fallback_ipv6.run_id')
 OR json_extract(OLD.address_json,'$.fallback_ipv6.check_status') IS NOT json_extract(NEW.address_json,'$.fallback_ipv6.check_status')
 OR json_extract(OLD.address_json,'$.fallback_ipv6.error') IS NOT json_extract(NEW.address_json,'$.fallback_ipv6.error')
BEGIN
 UPDATE nodes SET location_ip_revision=OLD.location_ip_revision+1 WHERE id=NEW.id;
END;
-- +goose StatementEnd

-- +goose Down
-- Forward-only migration.
