-- +goose Up
INSERT INTO agents_state(key,value)
SELECT 'memory_version',coalesce(max(version),0) FROM agents_memory;

-- +goose Down
DELETE FROM agents_state WHERE key='memory_version';
