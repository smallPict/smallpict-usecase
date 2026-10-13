FROM php:8.2-cli-alpine

WORKDIR /app

RUN apk add --no-cache postgresql-dev libpng-dev \
    && docker-php-ext-install pdo pdo_pgsql

COPY --from=composer:latest /usr/bin/composer /usr/bin/composer

COPY composer.json ./
RUN composer install --no-dev --optimize-autoloader

COPY . .

EXPOSE 8004
CMD ["php", "-S", "0.0.0.0:8004", "-t", "public"]
