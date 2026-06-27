//go:build js && wasm

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"syscall/js"

	database "github.com/lightninglabs/go-wasmsqlite/example/generated"
)

func testBlobJS(this js.Value, p []js.Value) interface{} {
	go func() {
		fmt.Println("\n🧪 Testing BLOB/Binary data operations...")
		ctx := context.Background()

		// Test 1: Create user with avatar
		fmt.Println("\n📍 Test 1: Creating user with avatar...")
		user, err := queries.CreateUser(ctx, database.CreateUserParams{
			Username: "blob_test_user",
			Email:    "blob@test.com",
		})
		if err != nil {
			fmt.Printf("❌ Error creating user: %v\n", err)
			return
		}
		fmt.Printf("✅ Created user: ID=%d, Username=%s\n", user.ID, user.Username)

		// Generate random avatar data (simulating an image)
		avatarData := make([]byte, 1024) // 1KB avatar
		rand.Read(avatarData)

		// Create metadata as JSON
		metadata := map[string]interface{}{
			"theme":  "dark",
			"locale": "en-US",
			"preferences": map[string]bool{
				"notifications": true,
				"newsletter":    false,
			},
		}
		metadataBytes, _ := json.Marshal(metadata)

		// Test 2: Update user with avatar and metadata
		fmt.Println("\n📍 Test 2: Updating user avatar and metadata...")
		err = queries.UpdateUserAvatar(ctx, database.UpdateUserAvatarParams{
			Avatar:   avatarData,
			Metadata: metadataBytes,
			ID:       user.ID,
		})
		if err != nil {
			fmt.Printf("❌ Error updating avatar: %v\n", err)
			return
		}
		fmt.Printf("✅ Updated avatar (%d bytes) and metadata (%d bytes)\n", len(avatarData), len(metadataBytes))

		// Test 3: Retrieve user with avatar
		fmt.Println("\n📍 Test 3: Retrieving user with avatar...")
		userWithAvatar, err := queries.GetUserWithAvatar(ctx, user.ID)
		if err != nil {
			fmt.Printf("❌ Error getting user with avatar: %v\n", err)
			return
		}

		// Verify avatar data matches
		if userWithAvatar.Avatar != nil && bytes.Equal(userWithAvatar.Avatar, avatarData) {
			fmt.Printf("✅ Avatar retrieved correctly (%d bytes match)\n", len(userWithAvatar.Avatar))
		} else {
			fmt.Printf("❌ Avatar data mismatch!\n")
		}

		// Verify metadata
		if userWithAvatar.Metadata != nil {
			var retrievedMetadata map[string]interface{}
			if err := json.Unmarshal(userWithAvatar.Metadata, &retrievedMetadata); err == nil {
				fmt.Printf("✅ Metadata retrieved: %v\n", retrievedMetadata)
			}
		}

		// Test 4: Create attachment with binary data
		fmt.Println("\n📍 Test 4: Creating attachment with binary data...")

		// Generate larger binary data (simulating a file)
		fileData := make([]byte, 5*1024) // 5KB file
		rand.Read(fileData)

		// Generate thumbnail
		thumbnailData := make([]byte, 256) // 256 bytes thumbnail
		rand.Read(thumbnailData)

		// Calculate checksum
		checksum := sha256.Sum256(fileData)

		attachment, err := queries.CreateAttachment(ctx, database.CreateAttachmentParams{
			UserID:      user.ID,
			Filename:    "test_document.pdf",
			ContentType: sql.NullString{String: "application/pdf", Valid: true},
			Data:        fileData,
			Thumbnail:   thumbnailData,
			Size:        sql.NullInt64{Int64: int64(len(fileData)), Valid: true},
			Checksum:    checksum[:],
		})
		if err != nil {
			fmt.Printf("❌ Error creating attachment: %v\n", err)
			return
		}
		fmt.Printf("✅ Created attachment: ID=%d, Filename=%s, Size=%d bytes\n",
			attachment.ID, attachment.Filename, len(fileData))

		// Test 5: Retrieve attachment
		fmt.Println("\n📍 Test 5: Retrieving attachment...")
		retrievedAttachment, err := queries.GetAttachment(ctx, attachment.ID)
		if err != nil {
			fmt.Printf("❌ Error getting attachment: %v\n", err)
			return
		}

		// Verify data integrity
		if bytes.Equal(retrievedAttachment.Data, fileData) {
			fmt.Printf("✅ Attachment data retrieved correctly (%d bytes match)\n", len(retrievedAttachment.Data))
		} else {
			fmt.Printf("❌ Attachment data mismatch!\n")
		}

		// Verify checksum
		retrievedChecksum := sha256.Sum256(retrievedAttachment.Data)
		if bytes.Equal(retrievedChecksum[:], retrievedAttachment.Checksum) {
			fmt.Println("✅ Checksum verified successfully")
		} else {
			fmt.Println("❌ Checksum verification failed!")
		}

		// Test 6: Get just the attachment data
		fmt.Println("\n📍 Test 6: Getting attachment data only...")
		dataOnly, err := queries.GetAttachmentData(ctx, attachment.ID)
		if err != nil {
			fmt.Printf("❌ Error getting attachment data: %v\n", err)
			return
		}
		if bytes.Equal(dataOnly, fileData) {
			fmt.Printf("✅ Attachment data-only query successful (%d bytes)\n", len(dataOnly))
		}

		// Test 7: Update attachment thumbnail
		fmt.Println("\n📍 Test 7: Updating attachment thumbnail...")
		newThumbnail := make([]byte, 512) // Larger thumbnail
		rand.Read(newThumbnail)

		err = queries.UpdateAttachmentThumbnail(ctx, database.UpdateAttachmentThumbnailParams{
			Thumbnail: newThumbnail,
			ID:        attachment.ID,
		})
		if err != nil {
			fmt.Printf("❌ Error updating thumbnail: %v\n", err)
			return
		}
		fmt.Printf("✅ Updated thumbnail (%d bytes)\n", len(newThumbnail))

		// Test 8: List attachments by user
		fmt.Println("\n📍 Test 8: Listing attachments by user...")
		attachments, err := queries.ListAttachmentsByUser(ctx, user.ID)
		if err != nil {
			fmt.Printf("❌ Error listing attachments: %v\n", err)
			return
		}
		fmt.Printf("✅ Found %d attachment(s) for user\n", len(attachments))
		for _, att := range attachments {
			fmt.Printf("   - %s (%d bytes)\n", att.Filename, len(att.Data))
		}

		// Test 9: Handle NULL BLOBs
		fmt.Println("\n📍 Test 9: Testing NULL BLOB handling...")
		userWithNull, err := queries.CreateUser(ctx, database.CreateUserParams{
			Username: "null_blob_user",
			Email:    "null@test.com",
		})
		if err != nil {
			fmt.Printf("❌ Error creating user: %v\n", err)
			return
		}

		// Update with NULL avatar (empty byte slice)
		err = queries.UpdateUserAvatar(ctx, database.UpdateUserAvatarParams{
			Avatar:   nil,
			Metadata: nil,
			ID:       userWithNull.ID,
		})
		if err != nil {
			fmt.Printf("❌ Error updating with NULL blobs: %v\n", err)
			return
		}

		// Retrieve and verify NULLs
		retrievedNull, err := queries.GetUserWithAvatar(ctx, userWithNull.ID)
		if err != nil {
			fmt.Printf("❌ Error getting user with NULL blobs: %v\n", err)
			return
		}
		if retrievedNull.Avatar == nil && retrievedNull.Metadata == nil {
			fmt.Println("✅ NULL BLOBs handled correctly")
		} else {
			fmt.Printf("⚠️ Expected NULL BLOBs but got: avatar=%v, metadata=%v\n",
				retrievedNull.Avatar, retrievedNull.Metadata)
		}

		// Test 10: Large BLOB handling
		fmt.Println("\n📍 Test 10: Testing large BLOB (100KB)...")
		largeBlobData := make([]byte, 100*1024) // 100KB
		rand.Read(largeBlobData)

		largeAttachment, err := queries.CreateAttachment(ctx, database.CreateAttachmentParams{
			UserID:      user.ID,
			Filename:    "large_file.bin",
			ContentType: sql.NullString{String: "application/octet-stream", Valid: true},
			Data:        largeBlobData,
			Thumbnail:   nil,
			Size:        sql.NullInt64{Int64: int64(len(largeBlobData)), Valid: true},
			Checksum:    nil,
		})
		if err != nil {
			fmt.Printf("❌ Error creating large attachment: %v\n", err)
			return
		}
		fmt.Printf("✅ Created large attachment: %d bytes\n", len(largeBlobData))

		// Verify large BLOB retrieval
		retrievedLarge, err := queries.GetAttachment(ctx, largeAttachment.ID)
		if err != nil {
			fmt.Printf("❌ Error retrieving large attachment: %v\n", err)
			return
		}
		if bytes.Equal(retrievedLarge.Data, largeBlobData) {
			fmt.Println("✅ Large BLOB retrieved successfully")
		} else {
			fmt.Printf("❌ Large BLOB data mismatch! Expected %d bytes, got %d\n",
				len(largeBlobData), len(retrievedLarge.Data))
		}

		// Clean up
		fmt.Println("\n📍 Cleaning up...")
		err = queries.DeleteAttachment(ctx, attachment.ID)
		if err != nil {
			fmt.Printf("⚠️ Error deleting attachment: %v\n", err)
		}
		err = queries.DeleteAttachment(ctx, largeAttachment.ID)
		if err != nil {
			fmt.Printf("⚠️ Error deleting large attachment: %v\n", err)
		}

		fmt.Println("\n✅ All BLOB tests completed successfully!")
	}()

	return nil
}
