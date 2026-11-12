-- +goose Up
-- Do not merge/rename historical duplicate plans or take over shared names.
-- Only unambiguous owned groups with an available target name are normalized.
UPDATE user_groups SET name=(
 SELECT p.name FROM plan_access_ownership o JOIN plans p ON p.id=o.plan_id
 WHERE o.group_id=user_groups.id AND p.group_id=o.group_id
)
WHERE id IN (
 SELECT o.group_id FROM plan_access_ownership o JOIN plans p ON p.id=o.plan_id AND p.group_id=o.group_id
 WHERE trim(p.name)<>'' AND p.name=trim(p.name)
 AND NOT EXISTS(SELECT 1 FROM plans other WHERE other.id<>p.id AND lower(trim(other.name))=lower(trim(p.name)))
 AND NOT EXISTS(SELECT 1 FROM user_groups g WHERE g.id<>o.group_id AND lower(trim(g.name))=lower(trim(p.name)))
);
-- Protect new/renamed names even for concurrent SQL writers. Historical duplicates
-- remain editable without renaming; application checks additionally handle Unicode.
-- +goose StatementBegin
CREATE TRIGGER plans_name_unique_insert BEFORE INSERT ON plans
WHEN EXISTS(SELECT 1 FROM plans WHERE lower(trim(name))=lower(trim(NEW.name)))
BEGIN SELECT RAISE(ABORT,'plan name is already in use'); END;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE TRIGGER plans_name_unique_update BEFORE UPDATE OF name ON plans
WHEN NEW.name<>OLD.name AND EXISTS(SELECT 1 FROM plans WHERE id<>NEW.id AND lower(trim(name))=lower(trim(NEW.name)))
BEGIN SELECT RAISE(ABORT,'plan name is already in use'); END;
-- +goose StatementEnd
-- +goose Down
-- Forward-only migration.
