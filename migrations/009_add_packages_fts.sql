-- +goose Up
CREATE VIRTUAL TABLE packages_fts USING fts5(
    name,
    display_name,
    description,
    content='packages',
    content_rowid='id',
    tokenize='unicode61 remove_diacritics 2'
);

-- Populate FTS index from existing data
INSERT INTO packages_fts(rowid, name, display_name, description)
    SELECT id, name, COALESCE(display_name, ''), COALESCE(description, '')
    FROM packages;

-- Keep FTS in sync on INSERT
-- +goose StatementBegin
CREATE TRIGGER packages_fts_insert AFTER INSERT ON packages BEGIN
    INSERT INTO packages_fts(rowid, name, display_name, description)
        VALUES (NEW.id, NEW.name, COALESCE(NEW.display_name, ''), COALESCE(NEW.description, ''));
END;
-- +goose StatementEnd

-- Keep FTS in sync on UPDATE
-- +goose StatementBegin
CREATE TRIGGER packages_fts_update AFTER UPDATE ON packages BEGIN
    INSERT INTO packages_fts(packages_fts, rowid, name, display_name, description)
        VALUES ('delete', OLD.id, OLD.name, COALESCE(OLD.display_name, ''), COALESCE(OLD.description, ''));
    INSERT INTO packages_fts(rowid, name, display_name, description)
        VALUES (NEW.id, NEW.name, COALESCE(NEW.display_name, ''), COALESCE(NEW.description, ''));
END;
-- +goose StatementEnd

-- Keep FTS in sync on DELETE
-- +goose StatementBegin
CREATE TRIGGER packages_fts_delete AFTER DELETE ON packages BEGIN
    INSERT INTO packages_fts(packages_fts, rowid, name, display_name, description)
        VALUES ('delete', OLD.id, OLD.name, COALESCE(OLD.display_name, ''), COALESCE(OLD.description, ''));
END;
-- +goose StatementEnd

-- Index for active_installs sorting
CREATE INDEX idx_packages_active_installs ON packages(is_active, active_installs DESC);

-- +goose Down
DROP INDEX idx_packages_active_installs;
DROP TRIGGER packages_fts_delete;
DROP TRIGGER packages_fts_update;
DROP TRIGGER packages_fts_insert;
DROP TABLE packages_fts;
