import fs from 'fs';
import path from 'path';
import axios from 'axios';
import { v4 as uuidv4 } from 'uuid';
import { SmallPictClient, type ImageFormat } from '@smallpict/sdk';
import { config } from '../config/config.js';
import { pool } from '../database/db.js';

export interface ImageRecord {
  id: string;
  filename: string;
  url: string;
  size: number;
  size_origin: number;
  mime_type: string;
  created_at: string;
}

export async function processUpload(
  fileBuffer?: Buffer,
  base64Input?: string,
  originalFilename?: string,
  inputMimeType?: string
): Promise<ImageRecord> {
  let data: Buffer;
  let filename = originalFilename || '';
  let mimeType = inputMimeType || 'image/jpeg';

  if (base64Input && base64Input.trim()) {
    let b64 = base64Input.trim();
    if (b64.includes(';base64,')) {
      const parts = b64.split(';base64,');
      if (parts[0].startsWith('data:')) {
        mimeType = parts[0].substring(5);
      }
      b64 = parts[1];
    } else if (b64.startsWith('data:') && b64.includes(',')) {
      const parts = b64.split(',');
      mimeType = parts[0].substring(5).split(';')[0];
      b64 = parts[1];
    }

    b64 = b64.replace(/\s+/g, '');
    data = Buffer.from(b64, 'base64');
    if (!filename) {
      filename = `image${mimeToExtension(mimeType)}`;
    }
  } else if (fileBuffer && fileBuffer.length > 0) {
    data = fileBuffer;
    if (!filename) {
      filename = 'image.jpg';
    }
  } else {
    throw new Error('No image content provided in request');
  }

  if (data.length === 0) {
    throw new Error('Image data is empty');
  }

  const extName = path.extname(filename) || mimeToExtension(mimeType);
  let baseName = path.basename(filename, extName);
  if (!baseName) baseName = 'image';

  const epochSec = Math.floor(Date.now() / 1000);
  let timestampedFilename = `${baseName}_${epochSec}${extName}`;
  const imageId = uuidv4();
  const sizeOrigin = data.length;
  let size = sizeOrigin;
  let imageUrl = '';

  const mode = config.smallpict.mode;
  const hasCredentials = !!(config.smallpict.apiKey && config.smallpict.secretKey);

  if (hasCredentials) {
    const client = new SmallPictClient({
      apiKey: config.smallpict.apiKey,
      secretKey: config.smallpict.secretKey,
      baseUrl: config.smallpict.baseUrl,
    });

    const result = await client.optimize(data, {
      filename: timestampedFilename,
      mimeType,
      format: config.smallpict.format as ImageFormat,
      quality: config.smallpict.quality,
      maxDimension: config.smallpict.maxDimension,
      maxWidth: config.smallpict.maxWidth,
      maxHeight: config.smallpict.maxHeight,
      fit: config.smallpict.fit,
      lossless: config.smallpict.lossless,
      stripMetadata: config.smallpict.stripMetadata,
    });

    if (result.uploadUrl) {
      const putResponse = await axios.put(result.uploadUrl, data, {
        headers: {
          'Content-Type': mimeType,
        },
        validateStatus: () => true,
        timeout: 60000,
      });

      if (putResponse.status >= 300) {
        throw new Error(`S3 upload failed with status ${putResponse.status}`);
      }
    }

    let remoteUrl = result.url;
    const jobId = result.jobId;
    const status = (result.status || '').toLowerCase();

    if (jobId && (['pending', 'processing', 'queued'].includes(status) || !remoteUrl)) {
      for (let i = 0; i < 20; i++) {
        await new Promise((res) => setTimeout(res, 2000));
        try {
          const jobStatus = await client.getJobStatus(jobId);
          const st = (jobStatus.status || '').toLowerCase();
          if (['completed', 'succeeded', 'success', 'ready', 'done'].includes(st)) {
            if (jobStatus.url) {
              remoteUrl = jobStatus.url;
            }
            if (jobStatus.bytesSaved && jobStatus.bytesSaved > 0) {
              size = sizeOrigin - Number(jobStatus.bytesSaved);
            }
            break;
          }
        } catch {
          // ignore polling errors
        }
      }
    }

    if (!remoteUrl) {
      throw new Error('Failed to get optimized image URL from SmallPict');
    }

    if (mode === 'locale' || mode === 'local') {
      const cleanUrl = remoteUrl.includes('?') ? remoteUrl.substring(0, remoteUrl.indexOf('?')) : remoteUrl;
      const remoteExt = path.extname(cleanUrl).toLowerCase();
      if (remoteExt === '.webp') {
        timestampedFilename = `${baseName}_${epochSec}.webp`;
        mimeType = 'image/webp';
      } else if (remoteExt === '.avif') {
        timestampedFilename = `${baseName}_${epochSec}.avif`;
        mimeType = 'image/avif';
      } else if (remoteExt === '.png') {
        timestampedFilename = `${baseName}_${epochSec}.png`;
        mimeType = 'image/png';
      } else if (remoteExt === '.jpg' || remoteExt === '.jpeg') {
        timestampedFilename = `${baseName}_${epochSec}.jpg`;
        mimeType = 'image/jpeg';
      }

      const uploadDir = path.resolve(config.uploadPath);
      if (!fs.existsSync(uploadDir)) {
        fs.mkdirSync(uploadDir, { recursive: true });
      }

      const dlRes = await axios.get(remoteUrl, {
        responseType: 'arraybuffer',
        timeout: 60000,
      });

      if (dlRes.status >= 300) {
        throw new Error(`Failed to download compressed image from SmallPict: HTTP ${dlRes.status}`);
      }

      const destPath = path.join(uploadDir, timestampedFilename);
      fs.writeFileSync(destPath, Buffer.from(dlRes.data));

      imageUrl = `/uploads/${timestampedFilename}`;
      size = fs.statSync(destPath).size;
    } else {
      imageUrl = remoteUrl;
    }
  } else {
    const uploadDir = path.resolve(config.uploadPath);
    if (!fs.existsSync(uploadDir)) {
      fs.mkdirSync(uploadDir, { recursive: true });
    }

    const destPath = path.join(uploadDir, timestampedFilename);
    fs.writeFileSync(destPath, data);
    imageUrl = `/uploads/${timestampedFilename}`;
  }

  const record: ImageRecord = {
    id: imageId,
    filename: timestampedFilename,
    url: imageUrl,
    size: Number(size),
    size_origin: Number(sizeOrigin),
    mime_type: mimeType,
    created_at: new Date().toISOString(),
  };

  try {
    await pool.query(
      `INSERT INTO public.images (id, filename, url, size, size_origin, mime_type, created_at)
       VALUES ($1, $2, $3, $4, $5, $6, $7)`,
      [record.id, record.filename, record.url, record.size, record.size_origin, record.mime_type, record.created_at]
    );
  } catch (err: any) {
    console.warn('Warning: Failed to save image metadata to database:', err.message);
  }

  return record;
}

function mimeToExtension(mimeType: string): string {
  switch (mimeType.toLowerCase()) {
    case 'image/png':
      return '.png';
    case 'image/webp':
      return '.webp';
    case 'image/avif':
      return '.avif';
    case 'image/gif':
      return '.gif';
    case 'image/svg+xml':
      return '.svg';
    default:
      return '.jpg';
  }
}
