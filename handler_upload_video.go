package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
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

	// FIXME Ignore errors and default to `aspectRatio = "other"`
	aspectRatio, err := getVideoAspectRatioName(localTempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get aspect ratio", err)
		return
	}

	localProcessedFilepath, err := processVideoForFastStart(localTempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to process video file", err)
		return
	}
	defer os.Remove(localProcessedFilepath)

	localProcessedFile, err := os.Open(localProcessedFilepath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to open processed video file", err)
		return
	}
	defer localProcessedFile.Close()

	assetKey := aspectRatio + "/" + assetFilename

	_, err = cfg.s3Client.PutObject(
		r.Context(),
		&s3.PutObjectInput{
			Bucket:      aws.String(cfg.s3Bucket),
			Key:         aws.String(assetKey),
			Body:        localProcessedFile,
			ContentType: aws.String(mediaType),
		},
	)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to store video file in AWS S3", err)
		return
	}

	url := fmt.Sprintf("%s,%s", cfg.s3Bucket, assetKey)
	video.VideoURL = &url
	if err := cfg.db.UpdateVideo(video); err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't update video information in database", err)
		return
	}

	video, err = cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to generate presigned video URL", err)
		return
	}

	fmt.Println("Saved video file to AWS S3 and serving it with presigned URL:", video.VideoURL)

	respondWithJSON(w, http.StatusOK, video)
}

func generatePresignedURL(
	s3Client *s3.Client,
	bucket, key string,
	expireTime time.Duration,
) (string, error) {
	presignClient := s3.NewPresignClient(s3Client)
	presignedRequest, err := presignClient.PresignGetObject(
		context.TODO(),
		&s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		},
		s3.WithPresignExpires(expireTime),
	)
	if err != nil {
		return "", fmt.Errorf("error calling PresignGetObject: %w", err)
	}

	return presignedRequest.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil // errors.New("video URL is nil")
	}

	bucketAndKey := strings.Split(*video.VideoURL, ",")
	if len(bucketAndKey) != 2 {
		return video, errors.New("video URL not in the format 'bucket,key'")
	}

	bucket := bucketAndKey[0]
	key := bucketAndKey[1]

	videoURL, err := generatePresignedURL(cfg.s3Client, bucket, key, 5*time.Minute)
	if err != nil {
		return video, fmt.Errorf("error generating presigned URL: %w", err)
	}

	video.VideoURL = &videoURL
	return video, nil
}

func getPercentError(actual float64, expected float64) float64 {
	percentError := math.Abs(actual-expected) / math.Abs(expected) * 100.0
	fmt.Println("percent error:", percentError)
	return percentError
}

func getVideoAspectRatioName(filePath string) (string, error) {
	ffprobeCmd := exec.Command(
		"ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_streams", filePath,
	)

	var ffprobeOut bytes.Buffer
	ffprobeCmd.Stdout = &ffprobeOut

	if err := ffprobeCmd.Run(); err != nil {
		return "", fmt.Errorf("error running ffprobe: %w", err)
	}

	var ffprobeShowStreams struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(ffprobeOut.Bytes(), &ffprobeShowStreams); err != nil {
		return "", fmt.Errorf("error unmarshalling ffprobe output: %w", err)
	}

	if len(ffprobeShowStreams.Streams) < 1 {
		return "", errors.New("ffprobe output has no stream information: %w")
	}

	width := ffprobeShowStreams.Streams[0].Width
	height := ffprobeShowStreams.Streams[0].Height
	if width <= 0 || height <= 0 {
		return "", fmt.Errorf("video has nonpositive width (%d) or height (%d)", width, height)
	}

	const portraitAspectRatio = 9.0 / 16.0
	const landscapeAspectRatio = 16.0 / 9.0
	const maxPercentError = 1.0 // A percentage, not a ratio

	aspectRatioName := "other"
	aspectRatio := float64(width) / float64(height)
	if getPercentError(aspectRatio, portraitAspectRatio) <= maxPercentError {
		aspectRatioName = "portrait"
	} else if getPercentError(aspectRatio, landscapeAspectRatio) <= maxPercentError {
		aspectRatioName = "landscape"
	}

	return aspectRatioName, nil
}

func processVideoForFastStart(filePath string) (string, error) {
	processedFilePath := filePath + ".processing"

	ffmpegCmd := exec.Command(
		"ffmpeg",
		"-i", filePath,
		"-c", "copy",
		"-movflags", "faststart",
		"-f", "mp4",
		processedFilePath,
	)

	var stderr bytes.Buffer
	ffmpegCmd.Stderr = &stderr

	if err := ffmpegCmd.Run(); err != nil {
		return "", fmt.Errorf("error processing video: %s, %v", stderr.String(), err)
	}

	fileInfo, err := os.Stat(processedFilePath)
	if err != nil {
		return "", fmt.Errorf("could not stat processed file: %v", err)
	}
	if fileInfo.Size() == 0 {
		return "", fmt.Errorf("processed file is empty")
	}

	return processedFilePath, nil
}
