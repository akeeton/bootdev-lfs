package main

import (
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func getAssetPath(videoID uuid.UUID, mediaType string) (string, error) {
	ext, err := mediaTypeToExt(mediaType)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s%s", videoID, ext), nil
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func mediaTypeToExt(contentyTypeHeader string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(contentyTypeHeader)
	if err != nil {
		return "nil", fmt.Errorf("error parsing media type from Content-Type header '%s': %w", contentyTypeHeader, err)
	}

	validMediaTypes := map[string]struct{}{
		"image/jpeg": {},
		"image/png":  {},
	}

	_, ok := validMediaTypes[mediaType]
	if !ok {
		return "", fmt.Errorf("invalid media type '%s'", mediaType)
	}

	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("failed to get extension from media type '%s'", mediaType)
	}
	return "." + parts[1], nil
}
