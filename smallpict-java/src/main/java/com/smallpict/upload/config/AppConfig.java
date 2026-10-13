package com.smallpict.upload.config;

import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.ResourceHandlerRegistry;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

import java.io.File;

@Configuration
@ConfigurationProperties(prefix = "smallpict")
public class AppConfig implements WebMvcConfigurer {
    private String mode = "locale";
    private String baseUrl = "https://api.smallpict.app";
    private String apiKey = "";
    private String secretKey = "";
    private String format = "auto";
    private int quality = 80;
    private Integer maxDimension = 1600;
    private Integer maxWidth = 1920;
    private Integer maxHeight = 1080;
    private String fit = "cover";
    private boolean lossless = false;
    private boolean stripMetadata = true;
    private String uploadPath = "./uploads";

    public AppConfig() {
    }

    public String getMode() {
        return mode;
    }

    public void setMode(String mode) {
        this.mode = mode;
    }

    public String getBaseUrl() {
        return baseUrl;
    }

    public void setBaseUrl(String baseUrl) {
        this.baseUrl = baseUrl;
    }

    public String getApiKey() {
        return apiKey;
    }

    public void setApiKey(String apiKey) {
        this.apiKey = apiKey;
    }

    public String getSecretKey() {
        return secretKey;
    }

    public void setSecretKey(String secretKey) {
        this.secretKey = secretKey;
    }

    public String getFormat() {
        return format;
    }

    public void setFormat(String format) {
        this.format = format;
    }

    public int getQuality() {
        return quality;
    }

    public void setQuality(int quality) {
        this.quality = quality;
    }

    public Integer getMaxDimension() {
        return maxDimension;
    }

    public void setMaxDimension(Integer maxDimension) {
        this.maxDimension = maxDimension;
    }

    public Integer getMaxWidth() {
        return maxWidth;
    }

    public void setMaxWidth(Integer maxWidth) {
        this.maxWidth = maxWidth;
    }

    public Integer getMaxHeight() {
        return maxHeight;
    }

    public void setMaxHeight(Integer maxHeight) {
        this.maxHeight = maxHeight;
    }

    public String getFit() {
        return fit;
    }

    public void setFit(String fit) {
        this.fit = fit;
    }

    public boolean isLossless() {
        return lossless;
    }

    public void setLossless(boolean lossless) {
        this.lossless = lossless;
    }

    public boolean isStripMetadata() {
        return stripMetadata;
    }

    public void setStripMetadata(boolean stripMetadata) {
        this.stripMetadata = stripMetadata;
    }

    public String getUploadPath() {
        return uploadPath;
    }

    public void setUploadPath(String uploadPath) {
        this.uploadPath = uploadPath;
    }

    @Override
    public void addResourceHandlers(ResourceHandlerRegistry registry) {
        File uploadDir = new File(uploadPath);
        if (!uploadDir.exists()) {
            uploadDir.mkdirs();
        }
        registry.addResourceHandler("/uploads/**")
                .addResourceLocations("file:" + uploadDir.getAbsolutePath() + "/");
    }
}
