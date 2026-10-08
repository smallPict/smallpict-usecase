package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	AppName            string
	Port               string
	DBHost             string
	DBPort             string
	DBName             string
	DBUser             string
	DBPass             string
	DBSSLMode          string
	SmallPictMode      string
	SmallPictBaseURL   string
	SmallPictAPIKey    string
	SmallPictSecretKey string
	SmallPictFormat       string
	SmallPictQuality      int
	SmallPictMaxDimension int
	SmallPictMaxWidth     int
	SmallPictMaxHeight    int
	SmallPictFit          string
	SmallPictLossless     bool
	SmallPictStripMetadata bool
	UploadPath            string
}

var Cfg *Config

func LoadConfig() *Config {
	_ = godotenv.Load()

	quality, _ := strconv.Atoi(getEnv("SMALLPICT_QUALITY", "75"))
	if quality <= 0 {
		quality = 75
	}

	maxDim, _ := strconv.Atoi(getEnv("SMALLPICT_MAX_DIMENSION", "1600"))
	maxW, _ := strconv.Atoi(getEnv("SMALLPICT_MAX_WIDTH", "1920"))
	maxH, _ := strconv.Atoi(getEnv("SMALLPICT_MAX_HEIGHT", "1080"))
	lossless, _ := strconv.ParseBool(getEnv("SMALLPICT_LOSSLESS", "false"))
	stripMeta := getEnv("SMALLPICT_STRIP_METADATA", "true") != "false"

	Cfg = &Config{
		AppName:                getEnv("APP_NAME", "SmallPict Go Service"),
		Port:                   getEnv("SERVER_PORT", "8001"),
		DBHost:                 getEnv("DB_HOST", "localhost"),
		DBPort:                 getEnv("DB_PORT", "5432"),
		DBName:                 getEnv("DB_NAME", "smallpict"),
		DBUser:                 getEnv("DB_USER", "postgres"),
		DBPass:                 getEnv("DB_PASS", "postgres"),
		DBSSLMode:              getEnv("DB_SSLMODE", "disable"),
		SmallPictMode:          getEnv("SMALLPICT_MODE", "locale"),
		SmallPictBaseURL:       getEnv("SMALLPICT_BASE_URL", "https://api.smallpict.app"),
		SmallPictAPIKey:        getEnv("SMALLPICT_API_KEY", ""),
		SmallPictSecretKey:     getEnv("SMALLPICT_SECRET_KEY", ""),
		SmallPictFormat:        getEnv("SMALLPICT_FORMAT", "auto"),
		SmallPictQuality:       quality,
		SmallPictMaxDimension:  maxDim,
		SmallPictMaxWidth:      maxW,
		SmallPictMaxHeight:     maxH,
		SmallPictFit:           getEnv("SMALLPICT_FIT", "cover"),
		SmallPictLossless:      lossless,
		SmallPictStripMetadata: stripMeta,
		UploadPath:             getEnv("UPLOAD_PATH", "./uploads"),
	}

	return Cfg
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
