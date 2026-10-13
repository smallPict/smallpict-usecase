FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o smallpict-go main.go

FROM alpine:latest
WORKDIR /app
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /app/smallpict-go .

EXPOSE 8001
CMD ["./smallpict-go"]
