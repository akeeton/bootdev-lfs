package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const uploadLimit = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, uploadLimit)

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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	fmt.Println("uploading video file for video ID", videoID, "by user", userID)

	// const maxMemory = 10 << 30
	// if err := r.ParseMultipartForm(maxMemory); err != nil {
	// 	respondWithError(w, http.StatusBadRequest, "Couldn't parse video form file", err)
	// 	return
	// }

	formFile, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get video form file", err)
		return
	}
	defer formFile.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for video", nil)
		return
	}

	validMediaTypes := map[string]struct{}{
		"video/mp4": {},
	}

	mediaType, err := contentTypeToMediaType(contentType, validMediaTypes)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Incorrect Content-Type header", err)
		return
	}

	assetFilename, err := getAssetFilename(mediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to create asset filename", err)
		return
	}

	localTempFile, err := os.CreateTemp("", assetFilename)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to create video file", err)
		return
	}
	defer os.Remove(localTempFile.Name())
	defer localTempFile.Close()

	_, err = io.Copy(localTempFile, formFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to copy video contents to file", err)
		return
	}

	_, err = localTempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to set seek position of video file", err)
		return
	}

	_, err = cfg.s3Client.PutObject(
		r.Context(),
		&s3.PutObjectInput{
			Bucket:      aws.String(cfg.s3Bucket),
			Key:         aws.String(assetFilename),
			Body:        localTempFile,
			ContentType: aws.String(mediaType),
		},
	)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to store video file in AWS S3", err)
		return
	}

	url := cfg.getS3AssetURL(assetFilename)
	video.VideoURL = &url
	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't update video information in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
