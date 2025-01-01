package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
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

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	if err := r.ParseMultipartForm(maxMemory); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't parse thumbnail form file", err)
		return
	}

	formFile, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't get thumbnail form file", err)
		return
	}
	defer formFile.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for thumbnail", nil)
		return
	}

	validMediaTypes := map[string]struct{}{
		"image/jpeg": {},
		"image/png":  {},
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

	assetDiskPath := cfg.getAssetDiskPath(assetFilename)

	localFile, err := os.Create(assetDiskPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to create thumbnail file", err)
		return
	}
	defer localFile.Close()

	_, err = io.Copy(localFile, formFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to copy thumbnail contents to file", err)
		return
	}

	url := cfg.getAssetURL(assetFilename)
	video.ThumbnailURL = &url
	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't update video information in database", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
