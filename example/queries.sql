-- name: CreateUser :one
INSERT INTO users (username, email)
VALUES (?, ?)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users
WHERE id = ?;

-- name: ListUsers :many
SELECT * FROM users
ORDER BY created_at DESC;

-- name: CreatePost :one
INSERT INTO posts (user_id, title, content, published)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetPost :one
SELECT p.*, u.username
FROM posts p
JOIN users u ON p.user_id = u.id
WHERE p.id = ?;

-- name: ListPostsByUser :many
SELECT * FROM posts
WHERE user_id = ?
ORDER BY created_at DESC;

-- name: ListPosts :many
SELECT * FROM posts
ORDER BY created_at DESC;

-- name: UpdatePost :exec
UPDATE posts
SET title = ?, content = ?, published = ?
WHERE id = ?;

-- name: DeletePost :exec
DELETE FROM posts
WHERE id = ?;