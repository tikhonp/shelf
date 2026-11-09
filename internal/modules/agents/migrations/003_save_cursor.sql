-- +goose Up
INSERT INTO agents_state(key,value) VALUES('save_cursor',0);

ALTER TABLE agents_acceptances ADD COLUMN via_save INTEGER NOT NULL DEFAULT 0 CHECK (via_save IN (0,1));

-- +goose Down
ALTER TABLE agents_acceptances DROP COLUMN via_save;
DELETE FROM agents_state WHERE key='save_cursor';
