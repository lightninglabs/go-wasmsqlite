-- Drop indexes
DROP INDEX IF EXISTS idx_users_username;
DROP INDEX IF EXISTS idx_posts_published;
DROP INDEX IF EXISTS idx_posts_user_id;

-- Drop tables
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS users;