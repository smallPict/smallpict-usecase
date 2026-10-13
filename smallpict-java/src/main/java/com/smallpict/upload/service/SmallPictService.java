package com.smallpict.upload.service;

import com.smallpict.SmallPictClient;
import com.smallpict.models.FitMode;
import com.smallpict.models.ImageFormat;
import com.smallpict.models.JobStatusResult;
import com.smallpict.models.OptimizeOptions;
import com.smallpict.models.OptimizeResult;
import com.smallpict.upload.config.AppConfig;
import com.smallpict.upload.model.Image;
import com.smallpict.upload.repository.ImageRepository;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.stereotype.Service;
import org.springframework.web.multipart.MultipartFile;

import java.io.File;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.time.Duration;
import java.time.Instant;
import java.util.Base64;
import java.util.UUID;

@Service
public class SmallPictService {

    private static final Logger log = LoggerFactory.getLogger(SmallPictService.class);

    private final AppConfig appConfig;
    private final ImageRepository imageRepository;
    private final HttpClient httpClient = HttpClient.newBuilder()
            .connectTimeout(Duration.ofSeconds(30))
            .followRedirects(HttpClient.Redirect.ALWAYS)
            .build();

    public SmallPictService(AppConfig appConfig, ImageRepository imageRepository) {
        this.appConfig = appConfig;
        this.imageRepository = imageRepository;
    }

    public Image processUpload(MultipartFile multipartFile, String base64Input, String originalFilename) throws Exception {
        byte[] data;
        String filename = originalFilename != null ? originalFilename : "";
        String mimeType = "image/jpeg";

        if (base64Input != null && !base64Input.trim().isEmpty()) {
            String b64 = base64Input.trim();
            if (b64.contains(";base64,")) {
                String[] parts = b64.split(";base64,", 2);
                if (parts[0].startsWith("data:")) {
                    mimeType = parts[0].substring(5);
                }
                b64 = parts[1];
            } else if (b64.startsWith("data:") && b64.contains(",")) {
                String[] parts = b64.split(",", 2);
                mimeType = parts[0].substring(5).split(";")[0];
                b64 = parts[1];
            }

            b64 = b64.replaceAll("\\s+", "");
            data = Base64.getDecoder().decode(b64);

            if (filename.isEmpty()) {
                filename = "image" + getExtensionFromMime(mimeType);
            }
        } else if (multipartFile != null && !multipartFile.isEmpty()) {
            data = multipartFile.getBytes();
            if (filename.isEmpty()) {
                filename = multipartFile.getOriginalFilename() != null ? multipartFile.getOriginalFilename() : "image.jpg";
            }
            if (multipartFile.getContentType() != null) {
                mimeType = multipartFile.getContentType();
            }
        } else {
            throw new IllegalArgumentException("No image data provided");
        }

        String baseName = filename.contains(".") ? filename.substring(0, filename.lastIndexOf('.')) : filename;
        String ext = filename.contains(".") ? filename.substring(filename.lastIndexOf('.')) : getExtensionFromMime(mimeType);
        if (baseName.isEmpty()) baseName = "image";

        long epochSec = Instant.now().getEpochSecond();
        String timestampedFilename = baseName + "_" + epochSec + ext;
        String imageId = UUID.randomUUID().toString();
        long sizeOrigin = data.length;
        long size = sizeOrigin;
        String imageUrl = "";
        String mode = appConfig.getMode().toLowerCase();

        boolean hasCredentials = appConfig.getApiKey() != null && !appConfig.getApiKey().isEmpty()
                && appConfig.getSecretKey() != null && !appConfig.getSecretKey().isEmpty();

        if (hasCredentials) {
            SmallPictClient client = SmallPictClient.builder()
                    .apiKey(appConfig.getApiKey())
                    .secretKey(appConfig.getSecretKey())
                    .baseUrl(appConfig.getBaseUrl())
                    .timeout(Duration.ofSeconds(60))
                    .build();

            OptimizeOptions.Builder optBuilder = OptimizeOptions.builder()
                    .filename(timestampedFilename)
                    .mimeType(mimeType)
                    .format(parseImageFormat(appConfig.getFormat()))
                    .quality(appConfig.getQuality())
                    .fit(parseFitMode(appConfig.getFit()))
                    .lossless(appConfig.isLossless())
                    .stripMetadata(appConfig.isStripMetadata());

            if (appConfig.getMaxDimension() != null && appConfig.getMaxDimension() > 0) {
                optBuilder.maxDimension(appConfig.getMaxDimension());
            }
            if (appConfig.getMaxWidth() != null && appConfig.getMaxWidth() > 0) {
                optBuilder.maxWidth(appConfig.getMaxWidth());
            }
            if (appConfig.getMaxHeight() != null && appConfig.getMaxHeight() > 0) {
                optBuilder.maxHeight(appConfig.getMaxHeight());
            }

            OptimizeOptions options = optBuilder.build();

            log.info("Requesting SmallPict optimization for: {}", timestampedFilename);
            OptimizeResult result = client.optimize(data, options);
            log.info("Ticket received, Job ID: {}", result.getJobId());

            OptimizedImageResult optResult = uploadAndPoll(client, result, data, mimeType);
            String remoteUrl = optResult.url;

            if (optResult.bytesSaved != null && optResult.bytesSaved > 0) {
                size = sizeOrigin - optResult.bytesSaved;
            } else if (optResult.compressedSize != null && optResult.compressedSize > 0) {
                size = optResult.compressedSize;
            }

            if (remoteUrl == null || remoteUrl.isEmpty()) {
                throw new RuntimeException("Failed to get optimized image URL from SmallPict");
            }

            if ("locale".equals(mode) || "local".equals(mode)) {
                String cleanUrlPath = remoteUrl.contains("?") ? remoteUrl.substring(0, remoteUrl.indexOf('?')) : remoteUrl;
                if (cleanUrlPath.lastIndexOf('.') > 0) {
                    String remoteExt = cleanUrlPath.substring(cleanUrlPath.lastIndexOf('.')).toLowerCase();
                    if (remoteExt.equals(".webp")) {
                        timestampedFilename = baseName + "_" + epochSec + ".webp";
                        mimeType = "image/webp";
                    } else if (remoteExt.equals(".avif")) {
                        timestampedFilename = baseName + "_" + epochSec + ".avif";
                        mimeType = "image/avif";
                    } else if (remoteExt.equals(".png")) {
                        timestampedFilename = baseName + "_" + epochSec + ".png";
                        mimeType = "image/png";
                    } else if (remoteExt.equals(".jpg") || remoteExt.equals(".jpeg")) {
                        timestampedFilename = baseName + "_" + epochSec + ".jpg";
                        mimeType = "image/jpeg";
                    }
                }

                Path uploadDirPath = Paths.get(appConfig.getUploadPath()).toAbsolutePath().normalize();
                Files.createDirectories(uploadDirPath);

                log.info("Downloading optimized image from S3: {}", remoteUrl);

                HttpRequest dlReq = HttpRequest.newBuilder()
                        .uri(URI.create(remoteUrl))
                        .GET()
                        .timeout(Duration.ofSeconds(60))
                        .build();
                HttpResponse<byte[]> dlRes = httpClient.send(dlReq, HttpResponse.BodyHandlers.ofByteArray());

                if (dlRes.statusCode() >= 300) {
                    String errBody = new String(dlRes.body(), StandardCharsets.UTF_8);
                    log.error("Failed to download compressed image from SmallPict: HTTP {}, body: {}", dlRes.statusCode(), errBody);
                    throw new RuntimeException("Failed to download compressed image from SmallPict: HTTP " + dlRes.statusCode());
                }

                File destFile = uploadDirPath.resolve(timestampedFilename).toFile();
                Files.write(destFile.toPath(), dlRes.body());

                imageUrl = "/uploads/" + timestampedFilename;
                size = destFile.length();
                log.info("Successfully saved local optimized file: {}, size: {} bytes", destFile.getAbsolutePath(), size);
            } else {
                imageUrl = remoteUrl;
            }
        } else {
            Path uploadDirPath = Paths.get(appConfig.getUploadPath()).toAbsolutePath().normalize();
            Files.createDirectories(uploadDirPath);
            File destFile = uploadDirPath.resolve(timestampedFilename).toFile();
            Files.write(destFile.toPath(), data);
            imageUrl = "/uploads/" + timestampedFilename;
        }

        Image imageEntity = Image.builder()
                .id(imageId)
                .filename(timestampedFilename)
                .url(imageUrl)
                .size(size)
                .sizeOrigin(sizeOrigin)
                .mimeType(mimeType)
                .build();

        return imageRepository.save(imageEntity);
    }

    /**
     * Upload binary image ke presigned S3 URL dan lakukan polling status job SmallPict
     */
    public OptimizedImageResult uploadAndPoll(SmallPictClient client, OptimizeResult result, byte[] data, String mimeType) throws Exception {
        if (result.getUploadUrl() != null && !result.getUploadUrl().isEmpty()) {
            log.info("Uploading image data to presigned S3 URL");
            HttpRequest putRequest = HttpRequest.newBuilder()
                    .uri(URI.create(result.getUploadUrl()))
                    .header("Content-Type", mimeType)
                    .PUT(HttpRequest.BodyPublishers.ofByteArray(data))
                    .timeout(Duration.ofSeconds(60))
                    .build();
            HttpResponse<Void> putResponse = httpClient.send(putRequest, HttpResponse.BodyHandlers.discarding());
            if (putResponse.statusCode() >= 300) {
                throw new RuntimeException("S3 upload failed with status " + putResponse.statusCode());
            }
        }

        String remoteUrl = result.getUrl();
        String jobId = result.getJobId();
        String status = result.getStatus() != null ? result.getStatus().toLowerCase() : "";
        Long bytesSaved = result.getBytesSaved();
        Long compressedSize = result.getCompressedSize();

        if (jobId != null && !jobId.isEmpty() && ("pending".equalsIgnoreCase(status) || "processing".equalsIgnoreCase(status) || "queued".equalsIgnoreCase(status) || remoteUrl == null || remoteUrl.isEmpty())) {
            log.info("Polling SmallPict job status for job_id: {}", jobId);
            for (int i = 0; i < 20; i++) {
                Thread.sleep(2000);
                try {
                    JobStatusResult jobStatus = client.getJobStatus(jobId);
                    String st = jobStatus.getStatus() != null ? jobStatus.getStatus().toLowerCase() : "";
                    if ("succeeded".equals(st) || "completed".equals(st) || "success".equals(st) || "ready".equals(st) || "done".equals(st)) {
                        if (jobStatus.getUrl() != null && !jobStatus.getUrl().isEmpty()) {
                            remoteUrl = jobStatus.getUrl();
                        }
                        if (jobStatus.getBytesSaved() != null && jobStatus.getBytesSaved() > 0) {
                            bytesSaved = jobStatus.getBytesSaved();
                        }
                        break;
                    }
                } catch (Exception e) {
                    log.warn("SmallPict poll attempt {} error: {}", i + 1, e.getMessage());
                }
            }
        }

        return new OptimizedImageResult(remoteUrl, bytesSaved, compressedSize);
    }

    private ImageFormat parseImageFormat(String format) {
        if (format == null) return ImageFormat.AUTO;
        return switch (format.toLowerCase()) {
            case "webp" -> ImageFormat.WEBP;
            case "avif" -> ImageFormat.AVIF;
            case "jpeg", "jpg" -> ImageFormat.JPEG;
            case "png" -> ImageFormat.PNG;
            default -> ImageFormat.AUTO;
        };
    }

    private FitMode parseFitMode(String fit) {
        if (fit == null) return FitMode.COVER;
        return switch (fit.toLowerCase()) {
            case "contain" -> FitMode.CONTAIN;
            case "inside" -> FitMode.INSIDE;
            case "outside" -> FitMode.OUTSIDE;
            default -> FitMode.COVER;
        };
    }

    private String getExtensionFromMime(String mimeType) {
        if (mimeType == null) return ".jpg";
        return switch (mimeType.toLowerCase()) {
            case "image/png" -> ".png";
            case "image/webp" -> ".webp";
            case "image/gif" -> ".gif";
            case "image/avif" -> ".avif";
            default -> ".jpg";
        };
    }

    public static class OptimizedImageResult {
        public final String url;
        public final Long bytesSaved;
        public final Long compressedSize;

        public OptimizedImageResult(String url, Long bytesSaved, Long compressedSize) {
            this.url = url;
            this.bytesSaved = bytesSaved;
            this.compressedSize = compressedSize;
        }
    }
}
