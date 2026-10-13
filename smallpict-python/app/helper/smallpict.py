import os
import time
import uuid
import base64
import re
import asyncio
import mimetypes
from typing import Optional, Tuple
import httpx
from sqlalchemy.orm import Session
from smallpict import AsyncSmallPictClient, OptimizeOptions
from app.config import settings
from app.models import Image

def get_extension_from_mime(mime_type: str) -> str:
    ext_map = {
        "image/jpeg": ".jpg",
        "image/png": ".png",
        "image/webp": ".webp",
        "image/avif": ".avif",
        "image/gif": ".gif"
    }
    return ext_map.get(mime_type, mimetypes.guess_extension(mime_type) or ".jpg")

def extract_image_data(
    file_bytes: Optional[bytes] = None,
    base64_str: Optional[str] = None,
    original_filename: Optional[str] = None
) -> Tuple[bytes, str, str]:
    data = b""
    filename = original_filename or ""
    mime_type = "image/jpeg"

    if base64_str:
        b64_content = base64_str.strip()
        if ";base64," in b64_content:
            meta, b64_content = b64_content.split(";base64,", 1)
            if meta.startswith("data:"):
                mime_type = meta.replace("data:", "")
        elif b64_content.startswith("data:") and "," in b64_content:
            meta, b64_content = b64_content.split(",", 1)
            mime_type = meta.replace("data:", "").split(";")[0]

        b64_clean = re.sub(r"\s+", "", b64_content)
        try:
            data = base64.b64decode(b64_clean)
        except Exception as e:
            raise ValueError(f"Invalid base64 string: {str(e)}")

        if not filename:
            ext = get_extension_from_mime(mime_type)
            filename = f"image{ext}"
    elif file_bytes:
        data = file_bytes
        if not filename:
            filename = "image.jpg"
        guessed_type, _ = mimetypes.guess_type(filename)
        if guessed_type:
            mime_type = guessed_type

    if not data:
        raise ValueError("Image data is empty")

    return data, filename, mime_type

async def upload(
    db: Session,
    file_bytes: Optional[bytes] = None,
    base64_str: Optional[str] = None,
    original_filename: Optional[str] = None
) -> Image:
    data, orig_filename, mime_type = extract_image_data(file_bytes, base64_str, original_filename)

    base_name, ext = os.path.splitext(orig_filename)
    if not base_name:
        base_name = "image"
    if not ext:
        ext = get_extension_from_mime(mime_type)

    timestamped_filename = f"{base_name}_{int(time.time())}{ext}"
    image_id = str(uuid.uuid4())
    size_origin = len(data)
    size = size_origin
    image_url = ""
    mode = settings.SMALLPICT_MODE.lower()

    if settings.SMALLPICT_API_KEY and settings.SMALLPICT_SECRET_KEY:
        client = AsyncSmallPictClient(
            api_key=settings.SMALLPICT_API_KEY,
            secret_key=settings.SMALLPICT_SECRET_KEY,
            base_url=settings.SMALLPICT_BASE_URL,
        )

        options = OptimizeOptions(
            filename=timestamped_filename,
            mime_type=mime_type,
            format=settings.SMALLPICT_FORMAT,
            quality=settings.SMALLPICT_QUALITY,
            max_dimension=settings.SMALLPICT_MAX_DIMENSION,
            max_width=settings.SMALLPICT_MAX_WIDTH,
            max_height=settings.SMALLPICT_MAX_HEIGHT,
            fit=settings.SMALLPICT_FIT,
            lossless=settings.SMALLPICT_LOSSLESS,
            strip_metadata=settings.SMALLPICT_STRIP_METADATA,
        )

        result = await client.optimize(data, options)

        if result.upload_url:
            async with httpx.AsyncClient(timeout=60.0) as http_c:
                put_headers = {"Content-Type": mime_type}
                put_resp = await http_c.put(result.upload_url, content=data, headers=put_headers)
                if put_resp.status_code >= 300:
                    raise RuntimeError(f"SmallPict S3 upload failed with status {put_resp.status_code}")

        remote_url = result.url or ""
        job_id = result.job_id
        status = (result.status or "").lower()

        if job_id and (status in ["pending", "processing", "queued"] or not remote_url):
            for _ in range(20):
                await asyncio.sleep(2)
                try:
                    job_data = await client.get_job_status(job_id)
                    job_status = (job_data.status or "").lower()
                    if job_status in ["succeeded", "completed", "success", "ready", "done"]:
                        if job_data.url:
                            remote_url = job_data.url
                        if job_data.bytes_saved and job_data.bytes_saved > 0:
                            size = size_origin - job_data.bytes_saved
                        break
                except Exception:
                    pass

        if not remote_url:
            raise RuntimeError("Failed to get optimized image URL from SmallPict")

        if mode in ["locale", "local"]:
            os.makedirs(settings.UPLOAD_PATH, exist_ok=True)
            async with httpx.AsyncClient(timeout=60.0, follow_redirects=True) as http_c:
                download_resp = await http_c.get(remote_url)
                if download_resp.status_code >= 300:
                    raise RuntimeError(f"Failed to download compressed image from SmallPict: {download_resp.status_code}")

                clean_url = remote_url.split("?")[0]
                remote_ext = os.path.splitext(clean_url)[1].lower()
                if remote_ext == ".webp":
                    timestamped_filename = f"{base_name}_{int(time.time())}.webp"
                    mime_type = "image/webp"
                elif remote_ext == ".avif":
                    timestamped_filename = f"{base_name}_{int(time.time())}.avif"
                    mime_type = "image/avif"
                elif remote_ext == ".png":
                    timestamped_filename = f"{base_name}_{int(time.time())}.png"
                    mime_type = "image/png"
                elif remote_ext in [".jpg", ".jpeg"]:
                    timestamped_filename = f"{base_name}_{int(time.time())}.jpg"
                    mime_type = "image/jpeg"

                dest_path = os.path.join(settings.UPLOAD_PATH, timestamped_filename)
                with open(dest_path, "wb") as f:
                    f.write(download_resp.content)

                image_url = f"/uploads/{timestamped_filename}"
                size = len(download_resp.content)
        else:
            image_url = remote_url
    else:
        os.makedirs(settings.UPLOAD_PATH, exist_ok=True)
        dest_path = os.path.join(settings.UPLOAD_PATH, timestamped_filename)
        with open(dest_path, "wb") as f:
            f.write(data)
        image_url = f"/uploads/{timestamped_filename}"

    image_record = Image(
        id=image_id,
        filename=timestamped_filename,
        url=image_url,
        size=size,
        size_origin=size_origin,
        mime_type=mime_type
    )

    db.add(image_record)
    db.commit()
    db.refresh(image_record)

    return image_record
