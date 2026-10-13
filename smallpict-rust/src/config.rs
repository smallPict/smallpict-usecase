use std::env;

#[derive(Clone, Debug)]
pub struct Config {
    pub app_name: String,
    pub app_port: u16,
    pub app_host: String,
    pub database_url: String,
    pub smallpict_mode: String,
    pub smallpict_base_url: String,
    pub smallpict_api_key: String,
    pub smallpict_secret_key: String,
    pub smallpict_format: String,
    pub smallpict_quality: u8,
    pub smallpict_max_dimension: Option<u32>,
    pub smallpict_max_width: Option<u32>,
    pub smallpict_max_height: Option<u32>,
    pub smallpict_fit: String,
    pub smallpict_lossless: bool,
    pub smallpict_strip_metadata: bool,
    pub upload_path: String,
}

impl Config {
    pub fn from_env() -> Self {
        if dotenvy::dotenv().is_err() {
            if let Ok(content) = std::fs::read_to_string(".env") {
                for line in content.lines() {
                    let line = line.trim();
                    if line.is_empty() || line.starts_with('#') {
                        continue;
                    }
                    if let Some((k, v)) = line.split_once('=') {
                        let key = k.trim();
                        let mut val = v.trim();
                        if let Some(stripped) = val.strip_prefix('"').and_then(|s| s.strip_suffix('"')) {
                            val = stripped;
                        } else if let Some(stripped) = val.strip_prefix('\'').and_then(|s| s.strip_suffix('\'')) {
                            val = stripped;
                        } else if let Some(idx) = val.find('#') {
                            val = val[..idx].trim();
                        }
                        if env::var(key).is_err() {
                            env::set_var(key, val);
                        }
                    }
                }
            }
        }

        let db_host = env::var("DB_HOST").unwrap_or_else(|_| "localhost".to_string());
        let db_port = env::var("DB_PORT").unwrap_or_else(|_| "5432".to_string());
        let db_user = env::var("DB_USER").unwrap_or_else(|_| "postgres".to_string());
        let db_pass = env::var("DB_PASS").unwrap_or_else(|_| "postgres".to_string());
        let db_name = env::var("DB_NAME").unwrap_or_else(|_| "smallpict".to_string());
        let db_ssl = env::var("DB_SSLMODE").unwrap_or_else(|_| "disable".to_string());

        let default_db_url = format!(
            "postgres://{}:{}@{}:{}/{}?sslmode={}",
            db_user, db_pass, db_host, db_port, db_name, db_ssl
        );

        let database_url = env::var("DATABASE_URL").unwrap_or(default_db_url);

        let smallpict_max_dimension = env::var("SMALLPICT_MAX_DIMENSION").ok().and_then(|v| v.parse().ok()).or(Some(1600));
        let smallpict_max_width = env::var("SMALLPICT_MAX_WIDTH").ok().and_then(|v| v.parse().ok()).or(Some(1920));
        let smallpict_max_height = env::var("SMALLPICT_MAX_HEIGHT").ok().and_then(|v| v.parse().ok()).or(Some(1080));
        let smallpict_fit = env::var("SMALLPICT_FIT").unwrap_or_else(|_| "cover".to_string());
        let smallpict_lossless = env::var("SMALLPICT_LOSSLESS").map(|v| v.eq_ignore_ascii_case("true")).unwrap_or(false);
        let smallpict_strip_metadata = env::var("SMALLPICT_STRIP_METADATA").map(|v| !v.eq_ignore_ascii_case("false")).unwrap_or(true);

        Self {
            app_name: env::var("APP_NAME").unwrap_or_else(|_| "SmallPict Rust Service".to_string()),
            app_port: env::var("APP_PORT").ok().and_then(|p| p.parse().ok()).unwrap_or(8007),
            app_host: env::var("APP_HOST").unwrap_or_else(|_| "0.0.0.0".to_string()),
            database_url,
            smallpict_mode: env::var("SMALLPICT_MODE").unwrap_or_else(|_| "locale".to_string()),
            smallpict_base_url: env::var("SMALLPICT_BASE_URL").unwrap_or_else(|_| "https://api.smallpict.app".to_string()),
            smallpict_api_key: env::var("SMALLPICT_API_KEY").unwrap_or_default(),
            smallpict_secret_key: env::var("SMALLPICT_SECRET_KEY").unwrap_or_default(),
            smallpict_format: env::var("SMALLPICT_FORMAT").unwrap_or_else(|_| "auto".to_string()),
            smallpict_quality: env::var("SMALLPICT_QUALITY").ok().and_then(|q| q.parse().ok()).unwrap_or(80),
            smallpict_max_dimension,
            smallpict_max_width,
            smallpict_max_height,
            smallpict_fit,
            smallpict_lossless,
            smallpict_strip_metadata,
            upload_path: env::var("UPLOAD_PATH").unwrap_or_else(|_| "./uploads".to_string()),
        }
    }
}
