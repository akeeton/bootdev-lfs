package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func contentTypeToMediaType(contentTypeHeader string, validMediaTypes map[string]struct{}) (string, error) {
	mediaType, _, err := mime.ParseMediaType(contentTypeHeader)
	if err != nil {
		return "nil", fmt.Errorf("error parsing media type from Content-Type header '%s': %w", contentTypeHeader, err)
	}

	_, ok := validMediaTypes[mediaType]
	if !ok {
		return "", fmt.Errorf("invalid media type '%s'", mediaType)
	}

	return mediaType, nil
}

func getAssetFilename(mediaType string) (string, error) {
	randomBytes := make([]byte, 32)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}

	randomBase64String := base64.RawURLEncoding.EncodeToString(randomBytes)
	ext := mediaTypeToExt(mediaType)

	return fmt.Sprintf("%s.%s", randomBase64String, ext), nil
}

func (cfg apiConfig) getAssetDiskPath(assetFilename string) string {
	return filepath.Join(cfg.assetsRoot, assetFilename)
}

func (cfg apiConfig) getAssetURL(assetFilename string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetFilename)
}

func (cfg apiConfig) getS3AssetURL(assetKey string) string {
	return fmt.Sprintf("%s/%s", cfg.s3CfDistribution, assetKey)
}

func mediaTypeToExt(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return "bin"
	}
	return parts[1]
}
