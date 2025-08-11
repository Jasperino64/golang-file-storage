package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	fmt.Println("uploading video", videoID, "by user", userID)
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "Video not found", err)
		return
	}

	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "You are not allowed to upload this video", nil)
		return
	}
	const uploadLimit = 1 << 30
	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	if header.Size > uploadLimit {
		respondWithError(w, http.StatusRequestEntityTooLarge, "File size exceeds limit", nil)
		return
	}
	mimeType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid media type", err)
		return
	}
	if mimeType != "video/mp4"{
		respondWithError(w, http.StatusUnsupportedMediaType, "Unsupported media type", nil)
		return
	}

	tempfile, err := os.CreateTemp("", "tubely-upload.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temporary file", err)
		return
	}
	defer os.Remove(tempfile.Name())
	defer tempfile.Close()
	
	io.Copy(tempfile, file)
	tempfile.Seek(0, io.SeekStart)

	aspectRatio, err := getVideoAspectRatio(tempfile.Name())
	
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get video aspect ratio", err)
		return
	}
	folder := ""
	switch aspectRatio {
	case "16:9":
		folder = "landscape"
	case "9:16":
		folder = "portrait"
	default:
		folder = "other"
	}
	
	processedFilePath, err := processVideoForFastStart(tempfile.Name())
	fmt.Println("processed file path:", processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to process video", err)
		return
	}
	processedFile, err := os.Open(processedFilePath)
	defer os.Remove(processedFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open processed video", err)
		return
	}
	bytes := make([]byte, 32)
	n, err := rand.Read(bytes) // Generate random bytes for uniqueness
	if err != nil || n != 32 {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate unique ID", err)
		return
	}
	
	fileKey := getAssetPath(mimeType)
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket: &cfg.s3Bucket,
		Key:    aws.String(fmt.Sprintf("%s/%s", folder, fileKey)),
		Body:   processedFile,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to upload video to S3", err)
		return
	}

	url := cfg.getObjectURL(folder, fileKey)
	video.VideoURL = &url
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to update video in database", err)
		return
	}
	respondWithJSON(w, http.StatusOK, video)
}
