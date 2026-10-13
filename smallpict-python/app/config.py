from typing import Optional
from dotenv import load_dotenv
from pydantic_settings import BaseSettings, SettingsConfigDict

# Ensure .env is loaded
load_dotenv()

class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

    APP_NAME: str = "SmallPict Python Service"
    APP_PORT: int = 8005
    APP_HOST: str = "0.0.0.0"

    # PostgreSQL Database Config
    DB_HOST: str = "localhost"
    DB_PORT: int = 5432
    DB_USER: str = "postgres"
    DB_PASS: str = "postgres"
    DB_NAME: str = "smallpict"
    DB_SSLMODE: str = "disable"

    # SmallPict Config
    SMALLPICT_MODE: str = "locale"  # locale | cdn
    SMALLPICT_BASE_URL: str = "https://api.smallpict.app"
    SMALLPICT_API_KEY: str = ""
    SMALLPICT_SECRET_KEY: str = ""
    SMALLPICT_FORMAT: str = "auto"
    SMALLPICT_QUALITY: int = 80

    # SmallPict API New Features
    SMALLPICT_MAX_DIMENSION: Optional[int] = 1600
    SMALLPICT_MAX_WIDTH: Optional[int] = 1920
    SMALLPICT_MAX_HEIGHT: Optional[int] = 1080
    SMALLPICT_FIT: str = "cover"
    SMALLPICT_LOSSLESS: bool = False
    SMALLPICT_STRIP_METADATA: bool = True

    UPLOAD_PATH: str = "./uploads"

    @property
    def database_url(self) -> str:
        return f"postgresql://{self.DB_USER}:{self.DB_PASS}@{self.DB_HOST}:{self.DB_PORT}/{self.DB_NAME}?sslmode={self.DB_SSLMODE}"

settings = Settings()

