//go:build js && wasm

package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"syscall/js"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	wasmsqlite "github.com/sputn1ck/go-sqlite3-wasm"
	database "github.com/sputn1ck/go-sqlite3-wasm/example/generated"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var (
	db      *sql.DB
	queries *database.Queries
)

func main() {
	fmt.Println("🚀 Starting go-sqlite3-wasm demo with golang-migrate...")

	// Initialize database connection once
	var err error
	db, err = openDB()
	if err != nil {
		log.Fatalf("❌ Failed to open database: %v", err)
	}

	// Run migrations
	fmt.Println("🔄 Running database migrations...")
	if err := runMigrations(db); err != nil {
		log.Fatalf("❌ Failed to run migrations: %v", err)
	}
	fmt.Println("✅ Migrations completed successfully!")

	queries = database.New(db)
	fmt.Println("✅ Database initialized!")

	// Set up global functions for JavaScript to call
	js.Global().Set("runDemo", js.FuncOf(runDemo))
	js.Global().Set("createUser", js.FuncOf(createUserJS))
	js.Global().Set("listUsers", js.FuncOf(listUsersJS))
	js.Global().Set("getUser", js.FuncOf(getUserJS))
	js.Global().Set("createPost", js.FuncOf(createPostJS))
	js.Global().Set("listPosts", js.FuncOf(listPostsJS))
	js.Global().Set("updatePost", js.FuncOf(updatePostJS))
	js.Global().Set("deletePost", js.FuncOf(deletePostJS))
	js.Global().Set("clearDatabase", js.FuncOf(clearDatabaseJS))
	js.Global().Set("dumpDatabase", js.FuncOf(dumpDatabaseJS))
	js.Global().Set("loadDatabase", js.FuncOf(loadDatabaseJS))
	js.Global().Set("getMigrationStatus", js.FuncOf(getMigrationStatusJS))

	fmt.Println("✅ Demo functions are ready!")
	fmt.Println("📖 Available functions:")
	fmt.Println("  - runDemo(): Run a complete demo with all CRUD operations")
	fmt.Println("  - createUser(username, email): Create a new user")
	fmt.Println("  - listUsers(): List all users")
	fmt.Println("  - getUser(id): Get a user by ID")
	fmt.Println("  - createPost(userID, title, content, published): Create a new post")
	fmt.Println("  - listPosts(userID): List posts by user")
	fmt.Println("  - updatePost(postID, title, content, published): Update a post")
	fmt.Println("  - deletePost(postID): Delete a post")
	fmt.Println("  - clearDatabase(): Clear all data from the database")
	fmt.Println("  - dumpDatabase(): Export database as SQL dump (saved to window.lastDatabaseDump)")
	fmt.Println("  - loadDatabase(dump): Import SQL dump to restore database")
	fmt.Println("  - getMigrationStatus(): Get current migration version and status")

	// Keep the program running
	select {}
}

func openDB() (*sql.DB, error) {
	// Try to open database - the Worker will handle VFS fallback
	db, err := sql.Open("wasmsqlite", "file=/demo.db?vfs=opfs-sahpool&busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return db, nil
}

func runMigrations(db *sql.DB) error {
	// Create source from embedded filesystem
	sourceDriver, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to create migration source: %w", err)
	}

	// Create custom database driver for WASM SQLite
	dbDriver, err := wasmsqlite.NewMigrateDriver(db)
	if err != nil {
		return fmt.Errorf("failed to create database driver: %w", err)
	}

	// Create migrate instance
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "wasmsqlite", dbDriver)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	// Get current version
	version, dirty, err := dbDriver.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	if dirty {
		fmt.Println("⚠️  Database is in dirty state, attempting to repair...")
		// Force set to the current version to clear dirty state
		if err := dbDriver.SetVersion(version, false); err != nil {
			return fmt.Errorf("failed to clear dirty state: %w", err)
		}
	}

	fmt.Printf("📌 Current migration version: %d\n", version)

	// Run migrations up to latest version
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	if err == migrate.ErrNoChange {
		fmt.Println("✅ Database is already up to date")
	} else {
		// Get new version
		newVersion, _, _ := dbDriver.Version()
		fmt.Printf("✅ Migrated to version: %d\n", newVersion)
	}

	return nil
}

func getMigrationStatusJS(this js.Value, p []js.Value) interface{} {
	go func() {
		driver, err := wasmsqlite.NewMigrateDriver(db)
		if err != nil {
			log.Printf("❌ Failed to create driver: %v", err)
			return
		}

		version, dirty, err := driver.Version()
		if err != nil {
			log.Printf("❌ Failed to get migration status: %v", err)
			return
		}

		if version == -1 {
			fmt.Println("📌 Migration Status: No migrations applied yet")
		} else {
			status := "clean"
			if dirty {
				status = "dirty"
			}
			fmt.Printf("📌 Migration Status: Version %d (%s)\n", version, status)
		}
	}()

	return nil
}

func runDemo(this js.Value, p []js.Value) interface{} {
	go func() {
		fmt.Println("\n🎬 Starting complete CRUD demo...")
		ctx := context.Background()

		// Clear existing data first
		fmt.Println("\n🧹 Clearing existing data...")
		db.ExecContext(ctx, "DELETE FROM posts")
		db.ExecContext(ctx, "DELETE FROM users")

		// CREATE: Create some users
		fmt.Println("\n👤 [CREATE] Creating users...")
		user1, err := queries.CreateUser(ctx, database.CreateUserParams{
			Username: "alice",
			Email:    "alice@example.com",
		})
		if err != nil {
			log.Printf("❌ Failed to create user 1: %v", err)
			return
		}
		fmt.Printf("✅ Created user: %s (ID: %d)\n", user1.Username, user1.ID)

		user2, err := queries.CreateUser(ctx, database.CreateUserParams{
			Username: "bob",
			Email:    "bob@example.com",
		})
		if err != nil {
			log.Printf("❌ Failed to create user 2: %v", err)
			return
		}
		fmt.Printf("✅ Created user: %s (ID: %d)\n", user2.Username, user2.ID)

		// READ: List all users
		fmt.Println("\n📋 [READ] Listing all users...")
		users, err := queries.ListUsers(ctx)
		if err != nil {
			log.Printf("❌ Failed to list users: %v", err)
			return
		}
		for _, user := range users {
			fmt.Printf("  - %s (%s) - ID: %d\n", user.Username, user.Email, user.ID)
		}

		// READ: Get specific user
		fmt.Println("\n🔍 [READ] Getting user by ID...")
		specificUser, err := queries.GetUser(ctx, user1.ID)
		if err != nil {
			log.Printf("❌ Failed to get user: %v", err)
			return
		}
		fmt.Printf("  Found user: %s (%s)\n", specificUser.Username, specificUser.Email)

		// CREATE: Create some posts
		fmt.Println("\n📝 [CREATE] Creating posts...")
		post1, err := queries.CreatePost(ctx, database.CreatePostParams{
			UserID:    user1.ID,
			Title:     "Hello World!",
			Content:   sql.NullString{String: "This is my first post using wasmsqlite!", Valid: true},
			Published: sql.NullBool{Bool: false, Valid: true}, // Start as draft
		})
		if err != nil {
			log.Printf("❌ Failed to create post 1: %v", err)
			return
		}
		fmt.Printf("✅ Created post: %s (ID: %d, Published: %t)\n", post1.Title, post1.ID, post1.Published.Bool)

		post2, err := queries.CreatePost(ctx, database.CreatePostParams{
			UserID:    user2.ID,
			Title:     "WASM is Amazing!",
			Content:   sql.NullString{String: "Running SQLite in the browser with Go WASM is incredible!", Valid: true},
			Published: sql.NullBool{Bool: true, Valid: true},
		})
		if err != nil {
			log.Printf("❌ Failed to create post 2: %v", err)
			return
		}
		fmt.Printf("✅ Created post: %s (ID: %d, Published: %t)\n", post2.Title, post2.ID, post2.Published.Bool)

		// UPDATE: Update the first post
		fmt.Println("\n✏️ [UPDATE] Updating Alice's post...")
		err = queries.UpdatePost(ctx, database.UpdatePostParams{
			ID:        post1.ID,
			Title:     "Hello World! (Updated)",
			Content:   sql.NullString{String: "This post has been updated to show that UPDATE works!", Valid: true},
			Published: sql.NullBool{Bool: true, Valid: true}, // Publish it
		})
		if err != nil {
			log.Printf("❌ Failed to update post: %v", err)
			return
		}
		fmt.Println("✅ Post updated successfully!")

		// READ: Get the updated post to verify
		updatedPost, err := queries.GetPost(ctx, post1.ID)
		if err != nil {
			log.Printf("❌ Failed to get updated post: %v", err)
			return
		}
		fmt.Printf("  Updated post: %s (Published: %t)\n", updatedPost.Title, updatedPost.Published.Bool)
		fmt.Printf("  Content: %s\n", updatedPost.Content.String)

		// READ: List posts by user
		fmt.Println("\n📄 [READ] Listing Alice's posts...")
		alicePosts, err := queries.ListPostsByUser(ctx, user1.ID)
		if err != nil {
			log.Printf("❌ Failed to list Alice's posts: %v", err)
			return
		}
		for _, post := range alicePosts {
			fmt.Printf("  - %s (Published: %t)\n", post.Title, post.Published.Bool)
		}

		// DELETE: Delete Bob's post
		fmt.Println("\n🗑️ [DELETE] Deleting Bob's post...")
		err = queries.DeletePost(ctx, post2.ID)
		if err != nil {
			log.Printf("❌ Failed to delete post: %v", err)
			return
		}
		fmt.Println("✅ Post deleted successfully!")

		// READ: List all posts to verify deletion
		fmt.Println("\n📋 [READ] Listing all remaining posts...")
		allPosts, err := queries.ListPosts(ctx)
		if err != nil {
			log.Printf("❌ Failed to list all posts: %v", err)
			return
		}
		fmt.Printf("  Total posts remaining: %d\n", len(allPosts))
		for _, post := range allPosts {
			fmt.Printf("  - %s by user %d\n", post.Title, post.UserID)
		}

		fmt.Println("\n🎉 Demo completed successfully!")
		fmt.Println("✅ All CRUD operations tested:")
		fmt.Println("  - CREATE: Users and Posts")
		fmt.Println("  - READ: List, Get by ID")
		fmt.Println("  - UPDATE: Post content and status")
		fmt.Println("  - DELETE: Remove posts")
		fmt.Println("💾 Data is persisted in OPFS and will survive page reloads!")
	}()

	return nil
}

func createUserJS(this js.Value, p []js.Value) interface{} {
	if len(p) < 2 {
		log.Println("❌ createUser requires username and email parameters")
		return nil
	}

	username := p[0].String()
	email := p[1].String()

	go func() {
		user, err := queries.CreateUser(context.Background(), database.CreateUserParams{
			Username: username,
			Email:    email,
		})
		if err != nil {
			log.Printf("❌ Failed to create user: %v", err)
			return
		}

		fmt.Printf("✅ Created user: %s (ID: %d)\n", user.Username, user.ID)
	}()

	return nil
}

func listUsersJS(this js.Value, p []js.Value) interface{} {
	go func() {
		users, err := queries.ListUsers(context.Background())
		if err != nil {
			log.Printf("❌ Failed to list users: %v", err)
			return
		}

		fmt.Println("\n📋 All users:")
		for _, user := range users {
			fmt.Printf("  - %s (%s) - ID: %d\n", user.Username, user.Email, user.ID)
		}
	}()

	return nil
}

func getUserJS(this js.Value, p []js.Value) interface{} {
	if len(p) < 1 {
		log.Println("❌ getUser requires id parameter")
		return nil
	}

	id := int64(p[0].Float())

	go func() {
		user, err := queries.GetUser(context.Background(), id)
		if err != nil {
			log.Printf("❌ Failed to get user: %v", err)
			return
		}

		fmt.Printf("\n🔍 User found: %s (%s) - ID: %d\n", user.Username, user.Email, user.ID)
	}()

	return nil
}

func createPostJS(this js.Value, p []js.Value) interface{} {
	if len(p) < 4 {
		log.Println("❌ createPost requires userID, title, content, and published parameters")
		return nil
	}

	userID := int64(p[0].Float())
	title := p[1].String()
	content := p[2].String()
	published := p[3].Bool()

	go func() {
		post, err := queries.CreatePost(context.Background(), database.CreatePostParams{
			UserID:    userID,
			Title:     title,
			Content:   sql.NullString{String: content, Valid: content != ""},
			Published: sql.NullBool{Bool: published, Valid: true},
		})
		if err != nil {
			log.Printf("❌ Failed to create post: %v", err)
			return
		}

		fmt.Printf("✅ Created post: %s (ID: %d)\n", post.Title, post.ID)
	}()

	return nil
}

func listPostsJS(this js.Value, p []js.Value) interface{} {
	if len(p) < 1 {
		log.Println("❌ listPosts requires userID parameter")
		return nil
	}

	userID := int64(p[0].Float())

	go func() {
		posts, err := queries.ListPostsByUser(context.Background(), userID)
		if err != nil {
			log.Printf("❌ Failed to list posts: %v", err)
			return
		}

		fmt.Printf("\n📄 Posts by user %d:\n", userID)
		for _, post := range posts {
			fmt.Printf("  - %s (Published: %t)\n", post.Title, post.Published.Bool)
		}
	}()

	return nil
}

func updatePostJS(this js.Value, p []js.Value) interface{} {
	if len(p) < 4 {
		log.Println("❌ updatePost requires postID, title, content, and published parameters")
		return nil
	}

	postID := int64(p[0].Float())
	title := p[1].String()
	content := p[2].String()
	published := p[3].Bool()

	go func() {
		err := queries.UpdatePost(context.Background(), database.UpdatePostParams{
			ID:        postID,
			Title:     title,
			Content:   sql.NullString{String: content, Valid: content != ""},
			Published: sql.NullBool{Bool: published, Valid: true},
		})
		if err != nil {
			log.Printf("❌ Failed to update post: %v", err)
			return
		}

		fmt.Printf("✅ Updated post ID %d: %s\n", postID, title)
	}()

	return nil
}

func deletePostJS(this js.Value, p []js.Value) interface{} {
	if len(p) < 1 {
		log.Println("❌ deletePost requires postID parameter")
		return nil
	}

	postID := int64(p[0].Float())

	go func() {
		err := queries.DeletePost(context.Background(), postID)
		if err != nil {
			log.Printf("❌ Failed to delete post: %v", err)
			return
		}

		fmt.Printf("✅ Deleted post ID %d\n", postID)
	}()

	return nil
}

func clearDatabaseJS(this js.Value, p []js.Value) interface{} {
	go func() {
		ctx := context.Background()

		// Delete all data
		_, err := db.ExecContext(ctx, "DELETE FROM posts")
		if err != nil {
			log.Printf("❌ Failed to delete posts: %v", err)
			return
		}

		_, err = db.ExecContext(ctx, "DELETE FROM users")
		if err != nil {
			log.Printf("❌ Failed to delete users: %v", err)
			return
		}

		fmt.Println("✅ Database cleared successfully!")
	}()

	return nil
}

func dumpDatabaseJS(this js.Value, p []js.Value) interface{} {
	go func() {
		dump, err := wasmsqlite.DumpDatabase(db)
		if err != nil {
			log.Printf("❌ Failed to dump database: %v", err)
			return
		}

		fmt.Println("✅ Database dumped successfully!")
		fmt.Printf("📄 Dump size: %d bytes\n", len(dump))

		// Call the JavaScript callback if it exists
		callback := js.Global().Get("onDatabaseDumped")
		if callback.Truthy() && callback.Type() == js.TypeFunction {
			callback.Invoke(dump)
		} else {
			// Fallback to setting global variable
			js.Global().Set("lastDatabaseDump", dump)
			fmt.Println("💾 Dump saved to window.lastDatabaseDump")
		}
	}()

	return nil
}

func loadDatabaseJS(this js.Value, p []js.Value) interface{} {
	if len(p) < 1 {
		log.Println("❌ loadDatabase requires SQL dump parameter")
		return nil
	}

	dump := p[0].String()

	go func() {
		err := wasmsqlite.LoadDatabase(db, dump)
		if err != nil {
			log.Printf("❌ Failed to load database: %v", err)
			return
		}

		fmt.Println("✅ Database loaded successfully!")
	}()

	return nil
}
