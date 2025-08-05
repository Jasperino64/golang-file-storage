package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)



func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	r.ParseMultipartForm(maxMemory)
	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
		return
	}
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusUnsupportedMediaType, "Unsupported media type", nil)
		return
	}
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not allowed to upload a thumbnail for this video", nil)
		return
	}

	// Generate a unique filename for the new thumbnail
	bytes := make([]byte, 32)
	n, err := rand.Read(bytes) // Generate random bytes for uniqueness
	if err != nil || n != 32 {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate unique ID", err)
		return
	}
	uniqueID := base64.RawURLEncoding.EncodeToString(bytes)
	if len(uniqueID) > 32 {
		uniqueID = uniqueID[:32] // Ensure the unique ID is not too long
	}
	assetPath := getAssetPath(uniqueID, mediaType)
	assetDiskPath := cfg.getAssetDiskPath(assetPath)

	// Remove the old thumbnail if it exists
	if video.ThumbnailURL != nil && *video.ThumbnailURL != "" {
		// Extract the asset path from the URL
		if idx := strings.LastIndex(*video.ThumbnailURL, "/assets/"); idx != -1 {
			oldAssetPath := (*video.ThumbnailURL)[idx+len("/assets/") : ]
			oldAssetDiskPath := cfg.getAssetDiskPath(oldAssetPath)
			if oldAssetDiskPath != assetDiskPath {
				_ = os.Remove(oldAssetDiskPath)
			}
		}
	}

	outFile, err := os.Create(assetDiskPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create file", err)
		return
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, file); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to copy file", err)
		return
	}

	// Set the thumbnail URL to the new file path
	thumbnailURL := cfg.getAssetURL(assetPath)
	video.ThumbnailURL = &thumbnailURL

	cfg.db.UpdateVideo(video)

	respondWithJSON(w, http.StatusOK, database.Video{
		ID:           video.ID,
		ThumbnailURL: video.ThumbnailURL,
		CreatedAt:    video.CreatedAt,
		UpdatedAt:    video.UpdatedAt,
		VideoURL:     video.VideoURL,
	})
}
