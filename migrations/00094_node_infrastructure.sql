-- +goose Up
-- Explicit private associations. Existing provider text and loose asset node_id
-- links are deliberately left untouched.
CREATE TABLE node_infrastructure (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    supplier_id INTEGER REFERENCES infra_suppliers(id),
    asset_id INTEGER UNIQUE REFERENCES infra_assets(id),
    revision INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX node_infrastructure_supplier ON node_infrastructure(supplier_id);

-- +goose Down
-- Forward-only: old releases cannot safely maintain shared billing.
