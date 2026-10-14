package helper

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"smallpict-go/config"
	"smallpict-go/database"
	"smallpict-go/model"

	"github.com/google/uuid"
	"github.com/smallpict/smallpict-go"
)

func ProcessUpload(fileHeader *multipart.FileHeader, base64Input string, customFilename string) (*model.Image, error) {
	var data []byte
	var filename string
	var mimeType string

	if base64Input != "" {
		b64Content := strings.TrimSpace(base64Input)
		if idx := strings.Index(b64Content, ";base64,"); idx != -1 {
			meta := b64Content[:idx]
			if strings.HasPrefix(meta, "data:") {
				mimeType = strings.TrimPrefix(meta, "data:")
			}
			b64Content = b64Content[idx+8:]
		} else if idx := strings.Index(b64Content, ","); idx != -1 && strings.HasPrefix(b64Content, "data:") {
			mimeType = strings.TrimPrefix(b64Content[:idx], "data:")
			b64Content = b64Content[idx+1:]
		}

		b64Content = strings.ReplaceAll(b64Content, " ", "")
		b64Content = strings.ReplaceAll(b64Content, "\n", "")
		b64Content = strings.ReplaceAll(b64Content, "\r", "")

		decoded, err := base64.StdEncoding.DecodeString(b64Content)
		if err != nil {
			decoded, err = base64.RawStdEncoding.DecodeString(b64Content)
		}
		if err != nil {
			decoded, err = base64.URLEncoding.DecodeString(b64Content)
		}
		if err != nil {
			return nil, fmt.Errorf("invalid base64 image data: %w", err)
		}
		data = decoded
		filename = customFilename
	} else if fileHeader != nil {
		f, err := fileHeader.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open uploaded file: %w", err)
		}
		defer f.Close()

		data, err = io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("failed to read uploaded file: %w", err)
		}

		filename = customFilename
		if filename == "" {
			filename = fileHeader.Filename
		}
		if mimeType == "" {
			mimeType = fileHeader.Header.Get("Content-Type")
		}
	} else {
		return nil, errors.New("no image content provided")
	}

	if len(data) == 0 {
		return nil, errors.New("image data is empty")
	}

	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}

	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	if base == "" {
		base = "image"
	}
	if ext == "" {
		ext = mimeToExtension(mimeType)
	}

	epochSec := time.Now().Unix()
	timestampedFilename := fmt.Sprintf("%s_%d%s", base, epochSec, ext)
	imageID := uuid.New().String()
	sizeOrigin := int64(len(data))
	size := sizeOrigin
	var imageURL string

	cfg := config.Cfg
	mode := strings.ToLower(cfg.SmallPictMode)
	if mode == "" {
		mode = "locale"
	}

	hasCredentials := cfg.SmallPictAPIKey != "" && cfg.SmallPictSecretKey != ""

	if hasCredentials {
		client, err := smallpict.NewClient(
			smallpict.WithAPIKey(cfg.SmallPictAPIKey),
			smallpict.WithSecretKey(cfg.SmallPictSecretKey),
			smallpict.WithBaseURL(cfg.SmallPictBaseURL),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize smallpict client: %w", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		maxWidth := cfg.SmallPictMaxWidth
		maxHeight := cfg.SmallPictMaxHeight
		if cfg.SmallPictMaxDimension > 0 {
			maxWidth = cfg.SmallPictMaxDimension
			maxHeight = cfg.SmallPictMaxDimension
		}

		opts := &smallpict.OptimizeOptions{
			Filename:      timestampedFilename,
			MIMEType:      mimeType,
			Format:        cfg.ResolveFormat(),
			Quality:       cfg.SmallPictQuality,
			MaxWidth:      maxWidth,
			MaxHeight:     maxHeight,
			Fit:           cfg.SmallPictFit,
			Lossless:      cfg.SmallPictLossless,
			StripMetadata: cfg.SmallPictStripMetadata,
		}

		result, err := client.Optimize(ctx, bytes.NewReader(data), opts)
		if err != nil {
			return nil, fmt.Errorf("smallpict optimize failed: %w", err)
		}

		if result.UploadURL != "" {
			req, err := http.NewRequestWithContext(ctx, http.MethodPut, result.UploadURL, bytes.NewReader(data))
			if err != nil {
				return nil, fmt.Errorf("failed to create s3 upload request: %w", err)
			}
			req.Header.Set("Content-Type", mimeType)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("failed to upload image to s3 presigned url: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("s3 upload returned status code %d", resp.StatusCode)
			}
		}

		remoteURL := result.URL
		if result.CompressedSize > 0 {
			size = result.CompressedSize
		}

		if result.JobID != "" && (result.Status == "pending" || result.Status == "processing" || remoteURL == "") {
			for i := 1; i <= 20; i++ {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(2 * time.Second):
				}

				status, err := client.GetJobStatus(ctx, result.JobID)
				if err != nil {
					continue
				}

				st := strings.ToLower(status.Status)
				if st == "succeeded" || st == "completed" || st == "success" || st == "ready" || st == "done" {
					if status.URL != "" {
						remoteURL = status.URL
					}
					if status.BytesSaved != nil && *status.BytesSaved > 0 {
						size = sizeOrigin - *status.BytesSaved
					}
					break
				}
			}
		}

		if remoteURL == "" {
			return nil, errors.New("failed to get optimized image URL from smallpict")
		}

		if mode == "locale" || mode == "local" {
			cleanURL := remoteURL
			if idx := strings.Index(cleanURL, "?"); idx != -1 {
				cleanURL = cleanURL[:idx]
			}
			remoteExt := strings.ToLower(filepath.Ext(cleanURL))
			if remoteExt == ".webp" {
				timestampedFilename = fmt.Sprintf("%s_%d.webp", base, epochSec)
				mimeType = "image/webp"
			} else if remoteExt == ".avif" {
				timestampedFilename = fmt.Sprintf("%s_%d.avif", base, epochSec)
				mimeType = "image/avif"
			} else if remoteExt == ".png" {
				timestampedFilename = fmt.Sprintf("%s_%d.png", base, epochSec)
				mimeType = "image/png"
			} else if remoteExt == ".jpg" || remoteExt == ".jpeg" {
				timestampedFilename = fmt.Sprintf("%s_%d.jpg", base, epochSec)
				mimeType = "image/jpeg"
			}

			uploadDir := cfg.UploadPath
			if uploadDir == "" {
				uploadDir = "./uploads"
			}
			_ = os.MkdirAll(uploadDir, 0755)

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create download request: %w", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, fmt.Errorf("failed to download compressed image from smallpict: %w", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, fmt.Errorf("download compressed image failed with status %d", resp.StatusCode)
			}

			optimizedData, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read downloaded compressed image: %w", err)
			}

			destPath := filepath.Join(uploadDir, timestampedFilename)
			if err := os.WriteFile(destPath, optimizedData, 0644); err != nil {
				return nil, fmt.Errorf("failed to save local file: %w", err)
			}

			imageURL = fmt.Sprintf("/uploads/%s", timestampedFilename)
			size = int64(len(optimizedData))
		} else {
			imageURL = remoteURL
		}
	} else {
		uploadDir := cfg.UploadPath
		if uploadDir == "" {
			uploadDir = "./uploads"
		}
		_ = os.MkdirAll(uploadDir, 0755)

		destPath := filepath.Join(uploadDir, timestampedFilename)
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return nil, fmt.Errorf("failed to save local file: %w", err)
		}

		imageURL = fmt.Sprintf("/uploads/%s", timestampedFilename)
	}

	record := model.Image{
		ID:         imageID,
		Filename:   &timestampedFilename,
		URL:        &imageURL,
		Size:       &size,
		SizeOrigin: &sizeOrigin,
		MIMEType:   &mimeType,
		CreatedAt:  time.Now(),
	}

	if database.DB != nil {
		if err := database.DB.Create(&record).Error; err != nil {
			return nil, fmt.Errorf("failed to save image metadata to database: %w", err)
		}
	}

	return &record, nil
}

func mimeToExtension(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/gif":
		return ".gif"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".jpg"
	}
}
