require 'base64'
require 'securerandom'
require 'fileutils'
require 'httparty'
require 'json'
require 'stringio'
require 'smallpict'
require_relative '../config'
require_relative '../models/image'

module SmallPictHelper
  def self.mime_to_ext(mime)
    case mime.to_s.downcase
    when 'image/png' then '.png'
    when 'image/webp' then '.webp'
    when 'image/gif' then '.gif'
    when 'image/avif' then '.avif'
    else '.jpg'
    end
  end

  def self.extract_data(file_hash: nil, base64_str: nil, filename: nil)
    data = nil
    fname = filename.to_s.delete("\0").strip
    mime = 'image/jpeg'

    if base64_str && !base64_str.strip.empty?
      b64 = base64_str.strip
      if b64.include?(';base64,')
        parts = b64.split(';base64,', 2)
        mime = parts[0].sub(/^data:/, '') if parts[0].start_with?('data:')
        b64 = parts[1]
      elsif b64.start_with?('data:') && b64.include?(',')
        parts = b64.split(',', 2)
        mime = parts[0].sub(/^data:/, '').split(';')[0]
        b64 = parts[1]
      end

      clean_b64 = b64.gsub(/\s+/, '')
      data = Base64.decode64(clean_b64)
      fname = "image#{mime_to_ext(mime)}" if fname.empty?
    elsif file_hash && file_hash[:tempfile]
      data = file_hash[:tempfile].read
      fname = file_hash[:filename].to_s.delete("\0").strip if fname.empty?
      mime = file_hash[:type] if file_hash[:type]
    end

    raise 'Image data is empty' if data.nil? || data.empty?

    [data, fname, mime]
  end

  def self.upload(file_hash: nil, base64_str: nil, original_filename: nil)
    data, orig_fname, mime_type = extract_data(
      file_hash: file_hash,
      base64_str: base64_str,
      filename: original_filename
    )

    clean_orig_fname = orig_fname.to_s.delete("\0").strip
    base = File.basename(clean_orig_fname, '.*')
    base = 'image' if base.empty?
    ext = File.extname(clean_orig_fname)
    ext = mime_to_ext(mime_type) if ext.empty?

    epoch_sec = Time.now.to_i
    timestamped_filename = "#{base}_#{epoch_sec}#{ext}"
    image_id = SecureRandom.uuid
    size_origin = data.bytesize
    size = size_origin
    image_url = ''
    mode = AppConfig.smallpict_mode

    has_credentials = !AppConfig.smallpict_api_key.empty? && !AppConfig.smallpict_secret_key.empty?

    if has_credentials
      client = SmallPict::Client.new(
        api_key: AppConfig.smallpict_api_key,
        secret_key: AppConfig.smallpict_secret_key,
        base_url: AppConfig.smallpict_base_url
      )

      result = client.optimize(
        StringIO.new(data),
        filename: timestamped_filename,
        mime_type: mime_type,
        format: AppConfig.smallpict_format,
        quality: AppConfig.smallpict_quality,
        max_dimension: AppConfig.smallpict_max_dimension,
        max_width: AppConfig.smallpict_max_width,
        max_height: AppConfig.smallpict_max_height,
        fit: AppConfig.smallpict_fit,
        lossless: AppConfig.smallpict_lossless,
        strip_metadata: AppConfig.smallpict_strip_metadata
      )

      if result.upload_url && !result.upload_url.empty?
        put_res = HTTParty.put(
          result.upload_url,
          body: data,
          headers: { 'Content-Type' => mime_type },
          timeout: 60
        )
        raise "SmallPict S3 upload failed with HTTP #{put_res.code}" if put_res.code >= 300
      end

      remote_url = result.url.to_s
      job_id = result.job_id
      status = result.status.to_s.downcase

      if job_id && (%w[pending processing queued].include?(status) || remote_url.empty?)
        20.times do
          sleep 2
          begin
            job_data = client.get_job_status(job_id)
            job_status = job_data.status.to_s.downcase
            if %w[succeeded completed success ready done].include?(job_status)
              remote_url = job_data.url.to_s if job_data.url
              if job_data.bytes_saved && job_data.bytes_saved.to_i > 0
                size = size_origin - job_data.bytes_saved.to_i
              end
              break
            elsif %w[failed error].include?(job_status)
              raise "SmallPict optimization failed: #{job_data.error || 'unknown error'}"
            end
          rescue StandardError => e
            raise e if e.message.start_with?('SmallPict optimization failed')
          end
        end
      end

      raise 'Failed to get optimized image URL from SmallPict' if remote_url.empty?

      if %w[locale local].include?(mode)
        clean_url = remote_url.include?('?') ? remote_url.split('?').first : remote_url
        remote_ext = File.extname(clean_url).downcase
        if remote_ext == '.webp'
          timestamped_filename = "#{base}_#{epoch_sec}.webp"
          mime_type = 'image/webp'
        elsif remote_ext == '.avif'
          timestamped_filename = "#{base}_#{epoch_sec}.avif"
          mime_type = 'image/avif'
        elsif remote_ext == '.png'
          timestamped_filename = "#{base}_#{epoch_sec}.png"
          mime_type = 'image/png'
        elsif %w[.jpg .jpeg].include?(remote_ext)
          timestamped_filename = "#{base}_#{epoch_sec}.jpg"
          mime_type = 'image/jpeg'
        end

        FileUtils.mkdir_p(AppConfig.upload_path)
        dl_res = HTTParty.get(remote_url, timeout: 60)
        raise "Failed to download compressed image HTTP #{dl_res.code}" if dl_res.code >= 300

        dest_path = File.join(AppConfig.upload_path, timestamped_filename)
        File.binwrite(dest_path, dl_res.body)

        image_url = "/uploads/#{timestamped_filename}"
        size = dl_res.body.bytesize
      else
        image_url = remote_url
      end
    else
      FileUtils.mkdir_p(AppConfig.upload_path)
      dest_path = File.join(AppConfig.upload_path, timestamped_filename)
      File.binwrite(dest_path, data)
      image_url = "/uploads/#{timestamped_filename}"
    end

    Image.create(
      id: image_id,
      filename: timestamped_filename,
      url: image_url,
      size: size,
      size_origin: size_origin,
      mime_type: mime_type,
      created_at: Time.now
    )
  end
end
