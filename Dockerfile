# ---- 构建阶段 ----
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/lulu-rpg ./cmd/server

# ---- 运行阶段 ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 lulu
WORKDIR /app
COPY --from=build /out/lulu-rpg /app/lulu-rpg
COPY web /app/web
ENV APP_WEB_DIR=/app/web \
    APP_DATA_DIR=/data \
    HTTP_ADDR=:8080
VOLUME /data
EXPOSE 8080
USER lulu
ENTRYPOINT ["/app/lulu-rpg"]
