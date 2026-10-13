<?php

namespace SmallPict\Config;

use Dotenv\Dotenv;

class Config
{
    private static ?Config $instance = null;

    public string $appName;
    public string $port;
    public string $dbHost;
    public string $dbPort;
    public string $dbName;
    public string $dbUser;
    public string $dbPass;
    public string $dbSslMode;
    public string $smallPictMode;
    public string $smallPictBaseUrl;
    public string $smallPictApiKey;
    public string $smallPictSecretKey;
    public string $smallPictFormat;
    public int $smallPictQuality;
    public ?int $smallPictMaxDimension;
    public ?int $smallPictMaxWidth;
    public ?int $smallPictMaxHeight;
    public string $smallPictFit;
    public bool $smallPictLossless;
    public bool $smallPictStripMetadata;
    public string $uploadPath;

    private function __construct()
    {
        $rootPath = dirname(__DIR__, 2);
        if (file_exists($rootPath . '/.env')) {
            $dotenv = Dotenv::createImmutable($rootPath);
            $dotenv->safeLoad();
        }

        $this->appName = $_ENV['APP_NAME'] ?? 'SmallPict PHP Service';
        $this->port = $_ENV['SERVER_PORT'] ?? '8004';
        $this->dbHost = $_ENV['DB_HOST'] ?? 'localhost';
        $this->dbPort = $_ENV['DB_PORT'] ?? '5432';
        $this->dbName = $_ENV['DB_NAME'] ?? 'smallpict';
        $this->dbUser = $_ENV['DB_USER'] ?? 'postgres';
        $this->dbPass = $_ENV['DB_PASS'] ?? 'postgres';
        $this->dbSslMode = $_ENV['DB_SSLMODE'] ?? 'disable';
        $this->smallPictMode = strtolower($_ENV['SMALLPICT_MODE'] ?? 'locale');
        $this->smallPictBaseUrl = rtrim($_ENV['SMALLPICT_BASE_URL'] ?? 'https://api.smallpict.app', '/');
        $this->smallPictApiKey = $_ENV['SMALLPICT_API_KEY'] ?? '';
        $this->smallPictSecretKey = $_ENV['SMALLPICT_SECRET_KEY'] ?? '';
        $this->smallPictFormat = $_ENV['SMALLPICT_FORMAT'] ?? 'auto';
        $this->smallPictQuality = (int)($_ENV['SMALLPICT_QUALITY'] ?? 80);
        $this->smallPictMaxDimension = isset($_ENV['SMALLPICT_MAX_DIMENSION']) ? (int)$_ENV['SMALLPICT_MAX_DIMENSION'] : 1600;
        $this->smallPictMaxWidth = isset($_ENV['SMALLPICT_MAX_WIDTH']) ? (int)$_ENV['SMALLPICT_MAX_WIDTH'] : 1920;
        $this->smallPictMaxHeight = isset($_ENV['SMALLPICT_MAX_HEIGHT']) ? (int)$_ENV['SMALLPICT_MAX_HEIGHT'] : 1080;
        $this->smallPictFit = $_ENV['SMALLPICT_FIT'] ?? 'cover';
        $this->smallPictLossless = filter_var($_ENV['SMALLPICT_LOSSLESS'] ?? false, FILTER_VALIDATE_BOOLEAN);
        $this->smallPictStripMetadata = filter_var($_ENV['SMALLPICT_STRIP_METADATA'] ?? true, FILTER_VALIDATE_BOOLEAN);
        $this->uploadPath = $_ENV['UPLOAD_PATH'] ?? './uploads';
    }

    public static function getInstance(): Config
    {
        if (self::$instance === null) {
            self::$instance = new self();
        }
        return self::$instance;
    }
}
