<?php

namespace SmallPict\Helper;

use SmallPict\Client;
use SmallPict\Config as SdkConfig;
use SmallPict\Models\OptimizeOptions;
use SmallPict\Config\Config;
use SmallPict\Database\Database;
use GuzzleHttp\Client as GuzzleClient;

class SmallPictHelper
{
    public static function processUpload(?array $fileItem, ?string $base64Input, ?string $customFilename): array
    {
        $data = null;
        $filename = $customFilename ?? '';
        $mimeType = 'image/jpeg';

        if (!empty($base64Input)) {
            $b64 = trim($base64Input);
            if (str_contains($b64, ';base64,')) {
                $parts = explode(';base64,', $b64, 2);
                if (str_starts_with($parts[0], 'data:')) {
                    $mimeType = substr($parts[0], 5);
                }
                $b64 = $parts[1];
            } elseif (str_starts_with($b64, 'data:') && str_contains($b64, ',')) {
                $parts = explode(',', $b64, 2);
                $mimeType = explode(';', substr($parts[0], 5))[0];
                $b64 = $parts[1];
            }

            $b64 = preg_replace('/\s+/', '', $b64);
            $decoded = base64_decode($b64, true);
            if ($decoded === false) {
                throw new \InvalidArgumentException('Invalid base64 image data');
            }
            $data = $decoded;
            if (empty($filename)) {
                $filename = 'image' . self::mimeToExtension($mimeType);
            }
        } elseif (!empty($fileItem) && isset($fileItem['tmp_name']) && file_exists($fileItem['tmp_name'])) {
            $data = file_get_contents($fileItem['tmp_name']);
            if (empty($filename)) {
                $filename = $fileItem['name'] ?? 'image.jpg';
            }
            if (!empty($fileItem['type'])) {
                $mimeType = $fileItem['type'];
            }
        } else {
            throw new \InvalidArgumentException('No image content provided in request');
        }

        if (empty($data)) {
            throw new \InvalidArgumentException('Image data is empty');
        }

        $baseName = pathinfo($filename, PATHINFO_FILENAME);
        $ext = pathinfo($filename, PATHINFO_EXTENSION);
        $ext = $ext ? '.' . $ext : self::mimeToExtension($mimeType);
        if (empty($baseName)) {
            $baseName = 'image';
        }

        $epochSec = time();
        $timestampedFilename = "{$baseName}_{$epochSec}{$ext}";
        $imageId = sprintf(
            '%04x%04x-%04x-%04x-%04x-%04x%04x%04x',
            mt_rand(0, 0xffff), mt_rand(0, 0xffff),
            mt_rand(0, 0xffff),
            mt_rand(0, 0x0fff) | 0x4000,
            mt_rand(0, 0x3fff) | 0x8000,
            mt_rand(0, 0xffff), mt_rand(0, 0xffff), mt_rand(0, 0xffff)
        );

        $sizeOrigin = strlen($data);
        $size = $sizeOrigin;
        $imageUrl = '';

        $config = Config::getInstance();
        $mode = strtolower($config->smallPictMode);
        $hasCredentials = !empty($config->smallPictApiKey) && !empty($config->smallPictSecretKey);

        if ($hasCredentials) {
            $sdkConfig = new SdkConfig($config->smallPictApiKey, $config->smallPictSecretKey, $config->smallPictBaseUrl);
            $client = new Client($sdkConfig);

            $optOptions = OptimizeOptions::fromArray([
                'format' => $config->smallPictFormat,
                'quality' => $config->smallPictQuality,
                'max_dimension' => $config->smallPictMaxDimension,
                'max_width' => $config->smallPictMaxWidth,
                'max_height' => $config->smallPictMaxHeight,
                'fit' => $config->smallPictFit,
                'lossless' => $config->smallPictLossless,
                'strip_metadata' => $config->smallPictStripMetadata,
                'filename' => $timestampedFilename,
                'mime_type' => $mimeType,
            ]);

            $stream = fopen('php://temp', 'r+');
            fwrite($stream, $data);
            rewind($stream);

            $result = $client->optimize($stream, $optOptions);
            if (is_resource($stream)) {
                fclose($stream);
            }

            if ($result->getUploadUrl()) {
                $ch = curl_init($result->getUploadUrl());
                curl_setopt_array($ch, [
                    CURLOPT_CUSTOMREQUEST  => 'PUT',
                    CURLOPT_POSTFIELDS     => $data,
                    CURLOPT_HTTPHEADER     => ["Content-Type: {$mimeType}"],
                    CURLOPT_RETURNTRANSFER => true,
                    CURLOPT_TIMEOUT        => 60,
                    CURLOPT_HTTP_VERSION   => CURL_HTTP_VERSION_1_1,
                    CURLOPT_IPRESOLVE      => CURL_IPRESOLVE_V4,
                ]);
                $putRes = curl_exec($ch);
                $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
                $err = curl_error($ch);
                curl_close($ch);

                if ($httpCode < 200 || $httpCode >= 300 || $putRes === false) {
                    throw new \RuntimeException("SmallPict S3 Upload failed HTTP {$httpCode}: {$err}");
                }
            }

            $remoteUrl = $result->getUrl();
            $jobId = $result->getJobId();
            $status = strtolower($result->getStatus());

            if (!empty($jobId) && (in_array($status, ['pending', 'processing', 'queued']) || empty($remoteUrl))) {
                for ($i = 0; $i < 20; $i++) {
                    usleep(2000000); // 2 seconds
                    $jobStatus = $client->getJobStatus($jobId);
                    $st = strtolower($jobStatus->getStatus());
                    if (in_array($st, ['completed', 'succeeded', 'success', 'ready', 'done'])) {
                        if ($jobStatus->getUrl()) {
                            $remoteUrl = $jobStatus->getUrl();
                        }
                        if ($jobStatus->getBytesSaved() && $jobStatus->getBytesSaved() > 0) {
                            $size = $sizeOrigin - $jobStatus->getBytesSaved();
                        }
                        break;
                    }
                }
            }

            if (empty($remoteUrl)) {
                throw new \RuntimeException('Failed to get optimized image URL from SmallPict');
            }

            if ($mode === 'locale' || $mode === 'local') {
                $cleanUrlPath = str_contains($remoteUrl, '?') ? substr($remoteUrl, 0, strpos($remoteUrl, '?')) : $remoteUrl;
                $remoteExt = strtolower(pathinfo($cleanUrlPath, PATHINFO_EXTENSION));
                if ($remoteExt === 'webp') {
                    $timestampedFilename = "{$baseName}_{$epochSec}.webp";
                    $mimeType = 'image/webp';
                } elseif ($remoteExt === 'avif') {
                    $timestampedFilename = "{$baseName}_{$epochSec}.avif";
                    $mimeType = 'image/avif';
                } elseif ($remoteExt === 'png') {
                    $timestampedFilename = "{$baseName}_{$epochSec}.png";
                    $mimeType = 'image/png';
                } elseif (in_array($remoteExt, ['jpg', 'jpeg'])) {
                    $timestampedFilename = "{$baseName}_{$epochSec}.jpg";
                    $mimeType = 'image/jpeg';
                }

                $uploadDir = $config->uploadPath;
                if (!is_dir($uploadDir)) {
                    mkdir($uploadDir, 0755, true);
                }

                $imageData = self::downloadImage($remoteUrl);
                $destPath = rtrim($uploadDir, '/') . '/' . $timestampedFilename;
                file_put_contents($destPath, $imageData);

                $imageUrl = "/uploads/{$timestampedFilename}";
                $size = filesize($destPath);
            } else {
                $imageUrl = $remoteUrl;
            }
        } else {
            $uploadDir = $config->uploadPath;
            if (!is_dir($uploadDir)) {
                mkdir($uploadDir, 0755, true);
            }

            $destPath = rtrim($uploadDir, '/') . '/' . $timestampedFilename;
            file_put_contents($destPath, $data);
            $imageUrl = "/uploads/{$timestampedFilename}";
        }

        $record = [
            'id'          => $imageId,
            'filename'    => $timestampedFilename,
            'url'         => $imageUrl,
            'size'        => $size,
            'size_origin' => $sizeOrigin,
            'mime_type'   => $mimeType,
            'created_at'  => date('Y-m-d H:i:s'),
        ];

        $pdo = Database::getConnection();
        if ($pdo !== null) {
            $stmt = $pdo->prepare("
                INSERT INTO public.images (id, filename, url, size, size_origin, mime_type, created_at)
                VALUES (:id, :filename, :url, :size, :size_origin, :mime_type, :created_at)
            ");
            $stmt->execute($record);
        }

        return $record;
    }

    private static function downloadImage(string $url): string
    {
        $lastError = '';
        for ($i = 0; $i < 3; $i++) {
            if ($i > 0) {
                usleep(500000); // 0.5s retry delay
            }

            $ch = curl_init($url);
            curl_setopt_array($ch, [
                CURLOPT_RETURNTRANSFER => true,
                CURLOPT_FOLLOWLOCATION => true,
                CURLOPT_TIMEOUT        => 60,
                CURLOPT_HTTP_VERSION   => CURL_HTTP_VERSION_1_1,
                CURLOPT_IPRESOLVE      => CURL_IPRESOLVE_V4,
                CURLOPT_SSL_VERIFYPEER => true,
                CURLOPT_SSL_VERIFYHOST => 2,
            ]);

            $response = curl_exec($ch);
            $httpCode = curl_getinfo($ch, CURLINFO_HTTP_CODE);
            $curlError = curl_error($ch);
            curl_close($ch);

            if ($response !== false && $httpCode >= 200 && $httpCode < 300) {
                return $response;
            }

            $lastError = $curlError ?: "HTTP {$httpCode}";
        }

        // Fallback using stream context
        $context = stream_context_create([
            'http' => [
                'timeout'          => 60,
                'protocol_version' => 1.1,
                'ignore_errors'    => true,
            ],
            'ssl' => [
                'verify_peer'      => false,
                'verify_peer_name' => false,
            ],
        ]);
        $streamContent = @file_get_contents($url, false, $context);
        if ($streamContent !== false && !empty($streamContent)) {
            return $streamContent;
        }

        throw new \RuntimeException("Failed to download compressed image from SmallPict: {$lastError}");
    }

    private static function mimeToExtension(string $mimeType): string
    {
        return match (strtolower($mimeType)) {
            'image/png' => '.png',
            'image/webp' => '.webp',
            'image/avif' => '.avif',
            'image/gif' => '.gif',
            'image/svg+xml' => '.svg',
            default => '.jpg',
        };
    }
}
