use crate::config::Config;
use crate::models::ImageRecord;
use base64::prelude::*;
use regex::Regex;
use reqwest::Client as ReqwestClient;
use smallpict::{Client as SmallPictClient, ImageFormat, OptimizeOptions};
use sqlx::PgPool;
use std::fs;
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};
use uuid::Uuid;

pub fn extract_image_data(
    file_bytes: Option<Vec<u8>>,
    base64_str: Option<String>,
    original_filename: Option<String>,
) -> Result<(Vec<u8>, String, String), String> {
    let mut data = Vec::new();
    let mut filename = original_filename.unwrap_or_default();
    let mut mime_type = "image/jpeg".to_string();

    if let Some(b64) = base64_str {
        let b64_trimmed = b64.trim();
        let mut b64_content = b64_trimmed;

        if let Some(idx) = b64_trimmed.find(";base64,") {
            let meta = &b64_trimmed[..idx];
            if meta.starts_with("data:") {
                mime_type = meta.replace("data:", "");
            }
            b64_content = &b64_trimmed[idx + 8..];
        } else if b64_trimmed.starts_with("data:") {
            if let Some(idx) = b64_trimmed.find(',') {
                let meta = &b64_trimmed[..idx];
                mime_type = meta.replace("data:", "").split(';').next().unwrap_or("image/jpeg").to_string();
                b64_content = &b64_trimmed[idx + 1..];
            }
        }

        let re = Regex::new(r"\s+").unwrap();
        let clean_b64 = re.replace_all(b64_content, "").to_string();

        data = BASE64_STANDARD
            .decode(&clean_b64)
            .map_err(|e| format!("Invalid base64 encoding: {}", e))?;

        if filename.is_empty() {
            let ext = mime_to_extension(&mime_type);
            filename = format!("image{}", ext);
        }
    } else if let Some(bytes) = file_bytes {
        data = bytes;
        if filename.is_empty() {
            filename = "image.jpg".to_string();
        }
        if let Some(mime) = mime_guess::from_path(&filename).first() {
            mime_type = mime.to_string();
        }
    }

    if data.is_empty() {
        return Err("No image content provided".to_string());
    }

    Ok((data, filename, mime_type))
}

pub fn mime_to_extension(mime: &str) -> &str {
    match mime.to_lowercase().as_str() {
        "image/jpeg" | "image/jpg" => ".jpg",
        "image/png" => ".png",
        "image/webp" => ".webp",
        "image/avif" => ".avif",
        "image/gif" => ".gif",
        "image/svg+xml" => ".svg",
        _ => ".jpg",
    }
}

pub fn string_to_image_format(format_str: &str) -> ImageFormat {
    match format_str.to_lowercase().as_str() {
        "webp" => ImageFormat::Webp,
        "avif" => ImageFormat::Avif,
        "png" => ImageFormat::Png,
        "jpeg" | "jpg" => ImageFormat::Jpeg,
        _ => ImageFormat::Auto,
    }
}

pub fn string_to_fit_mode(fit_str: &str) -> smallpict::models::FitMode {
    match fit_str.to_lowercase().as_str() {
        "contain" => smallpict::models::FitMode::Contain,
        "inside" => smallpict::models::FitMode::Inside,
        "outside" => smallpict::models::FitMode::Outside,
        _ => smallpict::models::FitMode::Cover,
    }
}

pub async fn upload_smallpict(
    pool: &PgPool,
    config: &Config,
    file_bytes: Option<Vec<u8>>,
    base64_str: Option<String>,
    original_filename: Option<String>,
) -> Result<ImageRecord, String> {
    let (data, orig_filename, mut mime_type) = extract_image_data(file_bytes, base64_str, original_filename)?;

    let path_obj = Path::new(&orig_filename);
    let mut base_name = path_obj.file_stem().and_then(|s| s.to_str()).unwrap_or("image").to_string();
    if base_name.is_empty() {
        base_name = "image".to_string();
    }

    let ext = path_obj.extension().and_then(|s| s.to_str()).map(|e| format!(".{}", e)).unwrap_or_else(|| mime_to_extension(&mime_type).to_string());

    let now_sec = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs();
    let mut timestamped_filename = format!("{}_{}{}", base_name, now_sec, ext);
    let image_id = Uuid::new_v4().to_string();
    let size_origin = data.len() as i64;
    let mut size = size_origin;
    let image_url: String;

    let mode = config.smallpict_mode.to_lowercase();
    let has_credentials = !config.smallpict_api_key.is_empty() && !config.smallpict_secret_key.is_empty();

    if has_credentials {
        let sp_client = SmallPictClient::builder()
            .api_key(&config.smallpict_api_key)
            .secret_key(&config.smallpict_secret_key)
            .base_url(&config.smallpict_base_url)
            .build()
            .map_err(|e| format!("SmallPict client error: {}", e))?;

        let mut opt_builder = OptimizeOptions::builder()
            .filename(&timestamped_filename)
            .mime_type(&mime_type)
            .format(string_to_image_format(&config.smallpict_format))
            .quality(config.smallpict_quality)
            .fit(string_to_fit_mode(&config.smallpict_fit))
            .lossless(config.smallpict_lossless)
            .strip_metadata(config.smallpict_strip_metadata);

        if let Some(dim) = config.smallpict_max_dimension {
            if dim > 0 {
                opt_builder = opt_builder.max_width(dim).max_height(dim);
            }
        } else {
            if let Some(w) = config.smallpict_max_width {
                opt_builder = opt_builder.max_width(w);
            }
            if let Some(h) = config.smallpict_max_height {
                opt_builder = opt_builder.max_height(h);
            }
        }

        let opt_opts = opt_builder.build();

        let result = sp_client
            .optimize(&data, Some(opt_opts))
            .await
            .map_err(|e| format!("SmallPict optimize error: {}", e))?;

        let mut remote_url = result.url.clone();

        if let Some(upload_url) = result.upload_url {
            let req_client = ReqwestClient::builder()
                .timeout(std::time::Duration::from_secs(60))
                .build()
                .map_err(|e| e.to_string())?;

            let put_res = req_client
                .put(&upload_url)
                .header("Content-Type", &mime_type)
                .body(data.clone())
                .send()
                .await
                .map_err(|e| format!("S3 upload error: {}", e))?;

            if !put_res.status().is_success() {
                return Err(format!("S3 upload failed status {}", put_res.status()));
            }
        }

        let job_id = result.job_id.clone();
        let status = result.status.to_lowercase();
        if status == "pending" || status == "processing" || status == "queued" || remote_url.is_empty() {
            for _ in 0..20 {
                tokio::time::sleep(tokio::time::Duration::from_secs(2)).await;
                if let Ok(job_status) = sp_client.get_job_status(&job_id).await {
                    let st = job_status.status.to_lowercase();
                    if st == "succeeded" || st == "completed" || st == "success" || st == "ready" || st == "done" {
                        if let Some(u) = job_status.url {
                            remote_url = u;
                        }
                        if let Some(saved) = job_status.bytes_saved {
                            size = size_origin - (saved as i64);
                        }
                        break;
                    }
                }
            }
        }

        if remote_url.is_empty() {
            return Err("Failed to retrieve optimized image URL from SmallPict".to_string());
        }

        if mode == "locale" || mode == "local" {
            let clean_url = remote_url.split('?').next().unwrap_or(&remote_url);
            let remote_ext = Path::new(clean_url).extension().and_then(|s| s.to_str()).unwrap_or("").to_lowercase();
            if remote_ext == "webp" {
                timestamped_filename = format!("{}_{}.webp", base_name, now_sec);
                mime_type = "image/webp".to_string();
            } else if remote_ext == "avif" {
                timestamped_filename = format!("{}_{}.avif", base_name, now_sec);
                mime_type = "image/avif".to_string();
            } else if remote_ext == "png" {
                timestamped_filename = format!("{}_{}.png", base_name, now_sec);
                mime_type = "image/png".to_string();
            } else if remote_ext == "jpg" || remote_ext == "jpeg" {
                timestamped_filename = format!("{}_{}.jpg", base_name, now_sec);
                mime_type = "image/jpeg".to_string();
            }

            fs::create_dir_all(&config.upload_path).map_err(|e| e.to_string())?;
            let req_client = ReqwestClient::builder()
                .timeout(std::time::Duration::from_secs(60))
                .build()
                .map_err(|e| e.to_string())?;

            let download_res = req_client
                .get(&remote_url)
                .send()
                .await
                .map_err(|e| format!("Failed to download image: {}", e))?;

            if !download_res.status().is_success() {
                return Err(format!("Download failed HTTP {}", download_res.status()));
            }

            let downloaded_bytes = download_res.bytes().await.map_err(|e| e.to_string())?;
            let dest_path = Path::new(&config.upload_path).join(&timestamped_filename);
            fs::write(&dest_path, &downloaded_bytes).map_err(|e| e.to_string())?;

            image_url = format!("/uploads/{}", timestamped_filename);
            size = downloaded_bytes.len() as i64;
        } else {
            image_url = remote_url;
        }
    } else {
        fs::create_dir_all(&config.upload_path).map_err(|e| e.to_string())?;
        let dest_path = Path::new(&config.upload_path).join(&timestamped_filename);
        fs::write(&dest_path, &data).map_err(|e| e.to_string())?;
        image_url = format!("/uploads/{}", timestamped_filename);
    }

    let record = sqlx::query_as::<_, ImageRecord>(
        r#"
        INSERT INTO public.images (id, filename, url, size, size_origin, mime_type, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, NOW())
        RETURNING id, filename, url, size, size_origin, mime_type, created_at
        "#,
    )
    .bind(&image_id)
    .bind(&timestamped_filename)
    .bind(&image_url)
    .bind(size)
    .bind(size_origin)
    .bind(&mime_type)
    .fetch_one(pool)
    .await
    .map_err(|e| format!("Database insert error: {}", e))?;

    Ok(record)
}
