# SmallPict Multi-Language Image Upload Mini-Services (Monorepo)

A collection of lightweight, production-ready image upload mini-services implemented across **7 different programming languages and frameworks**. Each service integrates seamlessly with the **[SmallPict Image Optimization API](https://smallpict.app)** to provide next-gen image transcoding (AVIF, WebP), smart compression, and storage management (local file storage or SmallPict Edge CDN).

---

## 🚀 Supported Languages & Frameworks

| Directory | Language / Framework | Database ORM / Driver | Default Port |
| :--- | :--- | :--- | :--- |
| **[`smallpict-go`](./smallpict-go)** | Go (Gin) | GORM PostgreSQL | `8001` |
| **[`smallpict-java`](./smallpict-java)** | Java 17 (Spring Boot 3) | Spring Data JPA / HikariCP | `8002` |
| **[`smallpict-js`](./smallpict-js)** | Node.js / TypeScript (Express) | `pg` Pool | `8003` |
| **[`smallpict-php`](./smallpict-php)** | PHP 8.1+ (Native MVC) | PDO PostgreSQL & Guzzle | `8004` |
| **[`smallpict-python`](./smallpict-python)** | Python 3.9+ (FastAPI) | SQLAlchemy | `8005` |
| **[`smallpict-ruby`](./smallpict-ruby)** | Ruby 3.0+ (Sinatra) | Sequel ORM | `8006` |
| **[`smallpict-rust`](./smallpict-rust)** | Rust (Axum 0.7) | SQLx (Async PostgreSQL) | `8007` |
| **[`wordpress`](./wordpress)** | WordPress CMS (Docker Compose) | MariaDB / MySQL | `8080` |

---

## 🌟 Common Architecture & Features

All services implement a unified API contract and consistent business logic:

1. **Dual Optimization Modes**:
   - `locale`: Optimizes the image through SmallPict, downloads the compressed result, and stores it in the local `./uploads` directory.
   - `cdn`: Uses the SmallPict Edge CDN URL directly for fast global delivery without using local disk storage.
2. **Dual Payload Ingestion**:
   - **`multipart/form-data`**: Upload binary image files directly via form-data (`image` or `file` field).
   - **`application/json`**: Upload Base64-encoded images (`image` or `base64` field).
3. **Database Logging**:
   - Automatically saves metadata (UUID, filename, URL, compressed size, original size, MIME type, created timestamp) into PostgreSQL (`public.images`).
4. **Health Check**:
   - `GET /health` endpoint for monitoring database connectivity and service readiness.

---

## 🗄️ Database Schema

All services share the same PostgreSQL table schema:

```sql
CREATE TABLE IF NOT EXISTS public.images (
    id VARCHAR(50) PRIMARY KEY,
    filename VARCHAR(255),
    url TEXT,
    size BIGINT,
    size_origin BIGINT,
    mime_type VARCHAR(100),
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);
```

---

## ⚙️ Environment Variables

Each subproject includes a `.env.example` file. Copy it to `.env` in the target directory:

```env
# Application Settings
APP_NAME="SmallPict Service"
APP_PORT=8001
APP_HOST=0.0.0.0

# PostgreSQL Database Configuration
DB_HOST=localhost
DB_PORT=5432
DB_NAME=smallpict
DB_USER=postgres
DB_PASS=postgres
DB_SSLMODE=disable

# SmallPict Configuration
SMALLPICT_MODE=locale # locale | cdn
SMALLPICT_BASE_URL=https://api.smallpict.app
SMALLPICT_API_KEY=your_api_key_here
SMALLPICT_SECRET_KEY=your_secret_key_here
SMALLPICT_FORMAT=auto # auto | webp | avif | jpeg | png
SMALLPICT_QUALITY=80 # 1 - 100

# SmallPict API New Features
SMALLPICT_MAX_DIMENSION=1600 # Max bounding dimension (1600, 1920, 2048, 2560, or 0 to keep full size / disable downscaling)
SMALLPICT_MAX_WIDTH=1920 # Max bounding width constraint in pixels
SMALLPICT_MAX_HEIGHT=1080 # Max bounding height constraint in pixels
SMALLPICT_FIT=cover # cover | contain | inside | outside
SMALLPICT_LOSSLESS=false # true | false
SMALLPICT_STRIP_METADATA=true # Strip EXIF, GPS, camera metadata (true | false)

# Local Storage Directory (used when SMALLPICT_MODE=locale)
UPLOAD_PATH=./uploads
```

> 💡 **Docker Compose**: Every service directory now contains its own ready-to-run `docker-compose.yml` bundling the application and a dedicated PostgreSQL database container with healthchecks and volume persistence. You can spin up any service with `docker compose up -d`.

---

## 📡 Unified API Specification

### 1. Base Service Info
- **Method:** `GET /`
- **Response:**
```json
{
  "status": "ok",
  "service": "SmallPict Python Service",
  "version": "1.0.0"
}
```

---

### 2. Database Health Check
- **Method:** `GET /health`
- **Response (`200 OK`):**
```json
{
  "status": "ok",
  "database": "connected",
  "timestamp": "2026-09-24T08:00:00Z"
}
```

---

### 3. Upload Image API
- **Method:** `POST /upload`

#### A. Multipart Form-Data (File Upload)
```bash
curl --location 'http://127.0.0.1/upload' \
  --form 'image=@"/path/to/image.jpg"'
```

#### B. JSON Payload (Base64)
```bash
curl --location 'http://127.0.0.1/upload' \
  --header 'Content-Type: application/json' \
  --data '{
    "name": "sample.jpg",
    "image": "data:image/jpeg;base64,/9j/4AAQSkZJRg..."
  }'
```

#### Success Response (`200 OK`):
```json
{
  "status": "success",
  "message": "Image uploaded successfully",
  "data": {
    "id": "88ebc5ca-ebde-4985-94b0-044fe7bb096a",
    "filename": "sample_1790236606.webp",
    "url": "/uploads/sample_1790236606.webp",
    "size": 43846,
    "size_origin": 111678,
    "mime_type": "image/webp",
    "created_at": "2026-09-24T14:56:49Z"
  }
}
```

---

## 🛠️ Quick Start by Language

### 1. Go (`smallpict-go`)
```bash
cd smallpict-go
cp .env.example .env
go mod tidy
go run main.go
```

### 2. Java Spring Boot (`smallpict-java`)
```bash
cd smallpict-java
cp .env.example .env
mvn spring-boot:run
```

### 3. Node.js / TypeScript (`smallpict-js`)
```bash
cd smallpict-js
cp .env.example .env
npm install
npm run dev
```

### 4. PHP (`smallpict-php`)
```bash
cd smallpict-php
cp .env.example .env
composer install
php -S localhost:8005 -t public
```

### 5. Python FastAPI (`smallpict-python`)
```bash
cd smallpict-python
cp .env.example .env
python -m venv venv
source venv/bin/activate
pip install -r requirements.txt
python main.py
```

### 6. Ruby Sinatra (`smallpict-ruby`)
```bash
cd smallpict-ruby
cp .env.example .env
bundle install
ruby app.rb
```

### 7. Rust Axum (`smallpict-rust`)
```bash
cd smallpict-rust
cp .env.example .env
cargo run
```

### 8. WordPress CMS (`wordpress`)
```bash
cd wordpress
# Configure database password & salts in docker-compose.yml
docker compose up -d
```
*(See complete step-by-step instructions in the [WordPress Local Environment (Docker Compose)](#-wordpress-local-environment-docker-compose) section below or in [wordpress/README.md](./wordpress/README.md))*

---

## 🐳 WordPress Local Environment (Docker Compose)

A step-by-step guide to running a local WordPress CMS instance using **Docker Compose** paired with a **MariaDB** database.

### 1. Prerequisites & Docker Installation
Before proceeding, ensure Docker and Docker Compose are installed on your machine:

- **macOS**:
  - Download and install [Docker Desktop for Mac](https://docs.docker.com/desktop/setup/install/mac-install/) (Apple Silicon / Intel).
  - Or via Homebrew: `brew install --cask docker`
  - Lightweight alternative: [OrbStack](https://orbstack.dev/) (`brew install orbstack`)
- **Linux (Ubuntu/Debian)**:
  ```bash
  curl -fsSL https://get.docker.com -o get-docker.sh && sudo sh get-docker.sh
  sudo usermod -aG docker $USER
  sudo apt-get update && sudo apt-get install docker-compose-plugin
  ```
- **Windows**:
  - Install [Docker Desktop for Windows](https://docs.docker.com/desktop/setup/install/windows-install/) with the WSL 2 backend.
- **Verify Installation**:
  ```bash
  docker --version
  docker compose version
  ```
  *(Note: Modern Docker Compose uses the `docker compose` syntax without a hyphen).*

---

### 2. Directory Structure (`wordpress/`)
```text
wordpress/
├── docker-compose.yml   # Service definitions for WordPress and MariaDB
├── uploads.ini          # Custom PHP runtime configuration (upload limits, timeouts, etc.)
├── wp-content/          # Local persistent volume (auto-created) for themes, plugins, and uploads
└── README.md            # Dedicated WordPress setup documentation
```

---

### 3. Database & Password Configuration
Open `smallpict-usecase/wordpress/docker-compose.yml` and configure database passwords in the `environment:` sections:

```yaml
services:
  wordpress:
    environment:
      - WORDPRESS_DB_NAME=wordpress
      - WORDPRESS_TABLE_PREFIX=wp_
      - WORDPRESS_DB_HOST=db
      - WORDPRESS_DB_USER=root
      - WORDPRESS_DB_PASSWORD=SecretPassword123!   # <-- Set your database password
      # ...
  db:
    image: mariadb:latest
    environment:
      - MYSQL_ROOT_PASSWORD=SecretPassword123!     # <-- Must EXACTLY match WORDPRESS_DB_PASSWORD
      - MYSQL_USER=wp_user                         # (Optional) Dedicated database user
      - MYSQL_PASSWORD=SecretPassword123!          # (Optional) Dedicated user password
      - MYSQL_DATABASE=wordpress
```

> **Important Rule**:
> - If `WORDPRESS_DB_USER` is set to `root`, then `WORDPRESS_DB_PASSWORD` **must exactly match** `MYSQL_ROOT_PASSWORD`.
> - If using a custom non-root user (`MYSQL_USER`), ensure `WORDPRESS_DB_USER` and `WORDPRESS_DB_PASSWORD` match `MYSQL_USER` and `MYSQL_PASSWORD`.

---

### 4. Generating & Configuring WordPress Salt Keys
WordPress Salt Keys are cryptographic random strings used to encrypt login cookies, authenticate user sessions, and generate secure nonces.

#### A. Generate Salt Keys
Generate random security keys directly via the official WordPress API:
- **Web Browser**: [https://api.wordpress.org/secret-key/1.1/salt/](https://api.wordpress.org/secret-key/1.1/salt/)
- **Terminal CLI**:
  ```bash
  curl -s https://api.wordpress.org/secret-key/1.1/salt/
  ```

The response output will be:
```php
define('AUTH_KEY',         'random_key_1');
define('SECURE_AUTH_KEY',  'random_key_2');
define('LOGGED_IN_KEY',    'random_key_3');
define('NONCE_KEY',        'random_key_4');
define('AUTH_SALT',        'random_key_5');
define('SECURE_AUTH_SALT', 'random_key_6');
define('LOGGED_IN_SALT',   'random_key_7');
define('NONCE_SALT',       'random_key_8');
```

#### B. Place Keys into `docker-compose.yml`
Copy each random string value and paste it into the respective environment variable under the `wordpress` service:
```yaml
      - WORDPRESS_AUTH_KEY=random_key_1
      - WORDPRESS_SECURE_AUTH_KEY=random_key_2
      - WORDPRESS_LOGGED_IN_KEY=random_key_3
      - WORDPRESS_NONCE_KEY=random_key_4
      - WORDPRESS_AUTH_SALT=random_key_5
      - WORDPRESS_SECURE_AUTH_SALT=random_key_6
      - WORDPRESS_LOGGED_IN_SALT=random_key_7
      - WORDPRESS_NONCE_SALT=random_key_8
```

> [!WARNING]
> **Important: Handling Dollar Signs (`$`) in Docker Compose**  
> If any generated salt string contains a dollar sign (`$`), Docker Compose will treat it as an environment variable and substitute it with an empty string.  
> **Solution**: Escape every `$` by doubling it to `$$` (e.g. `key$123` becomes `key$$123`), or re-run the `curl` generator command until you get a set without any `$` characters.

---

### 5. Running Containers
1. Navigate to the WordPress directory:
   ```bash
   cd smallpict-usecase/wordpress
   ```
2. Launch the services in detached mode:
   ```bash
   docker compose up -d
   ```
   *(or `docker-compose up -d` for legacy versions)*
3. Check container status:
   ```bash
   docker compose ps
   ```
   Ensure both `wordpress` and `db` services report a status of `Up`.
4. Monitor startup logs:
   ```bash
   docker compose logs -f
   ```
   *(Press `Ctrl + C` to exit the log stream).*

---

### 6. Completing Setup in the Browser
1. Open your web browser and visit:
   ```text
   http://localhost:8080
   ```
2. Complete the **WordPress Installation Wizard**:
   - Choose your preferred language (English, etc.).
   - Enter your **Site Title**, **Admin Username**, **Admin Password**, and **Email**.
   - Click **Install WordPress**.
3. Log into the WordPress Admin Dashboard at:
   ```text
   http://localhost:8080/wp-admin
   ```

---

### 7. PHP Upload Settings (`uploads.ini`)
PHP runtime limits are configured in `uploads.ini`:
```ini
file_uploads = On
memory_limit = 10M
upload_max_filesize = 10M
post_max_size = 20M
max_execution_time = 600
```
To increase limits for testing large image uploads (e.g. for SmallPict plugin optimization testing), edit `uploads.ini` (e.g. `upload_max_filesize = 64M`, `post_max_size = 128M`), then restart WordPress:
```bash
docker compose restart wordpress
```

---

### 8. Docker Management Commands

| Command | Description |
| :--- | :--- |
| `docker compose up -d` | Start all services in the background |
| `docker compose ps` | Check container health and status |
| `docker compose logs -f wordpress` | Stream real-time logs from WordPress |
| `docker compose stop` | Pause/stop running containers |
| `docker compose start` | Resume stopped containers |
| `docker compose restart` | Restart all containers |
| `docker compose down` | Stop and remove containers and network |
| `docker compose down -v` | **Reset Database**: Stop containers and permanently delete database volume `db_data` |
| `docker compose exec wordpress bash` | Open an interactive bash shell in the WordPress container |
| `docker compose exec db mariadb -u root -p` | Connect to the MariaDB interactive SQL CLI |

---

### 9. Troubleshooting & FAQ
- **Port 8080 Conflict / Already in Use**:
  Change the port mapping under `wordpress.ports` in `docker-compose.yml` from `8080:80` to another port, e.g. `8088:80`, then run `docker compose up -d`. Access WordPress at `http://localhost:8088`.
- **Error establishing a database connection**:
  On first run, MariaDB requires ~10–20 seconds to initialize default tables and grant user privileges. Wait a moment and refresh your browser. Verify that `WORDPRESS_DB_PASSWORD` and `MYSQL_ROOT_PASSWORD` match exactly.
- **Permission Denied on `wp-content/`**:
  If WordPress fails to upload files or install plugins due to host directory permissions, adjust folder ownership:
  ```bash
  sudo chown -R 33:33 wp-content
  ```
  *(UID 33 is the default `www-data` user inside the WordPress container).*

---

## 📄 License
MIT License. Open-source and free to use.
