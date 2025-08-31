-- Add updated_at columns to track modifications
ALTER TABLE users ADD COLUMN updated_at TIMESTAMP;
ALTER TABLE posts ADD COLUMN updated_at TIMESTAMP;

-- Initialize updated_at with created_at values for existing rows
UPDATE users SET updated_at = created_at WHERE updated_at IS NULL;
UPDATE posts SET updated_at = created_at WHERE updated_at IS NULL;