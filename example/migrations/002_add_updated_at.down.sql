-- SQLite doesn't support DROP COLUMN directly, so we need to recreate the tables
-- This is a limitation of SQLite, but for demonstration purposes we'll use a pragma

-- Note: In production, you'd want to:
-- 1. Create new tables without the column
-- 2. Copy data from old tables
-- 3. Drop old tables
-- 4. Rename new tables

-- For this demo, we'll just document that this migration is not reversible in SQLite
-- without recreating the tables
SELECT 'This migration cannot be reversed without recreating tables in SQLite';