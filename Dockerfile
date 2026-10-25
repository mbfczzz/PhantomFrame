FROM node:22-bookworm-slim AS web
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY index.html vite.config.ts tsconfig.json ./
COPY src ./src
COPY public ./public
RUN npm run build:web

FROM golang:1.26-bookworm AS api
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY server/assets.go server/reference-models.json server/demo-video.webm ./server/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /phantomframe ./cmd/api

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg && rm -rf /var/lib/apt/lists/* && useradd --system --uid 10001 --create-home app
WORKDIR /app
COPY --from=api /phantomframe /app/phantomframe
COPY --from=web /app/dist /app/dist
RUN mkdir /app/data && chown app:app /app/data
ENV APP_ENV=production HOST=0.0.0.0 PORT=3001 DATA_DIR=/app/data FRONTEND_DIR=/app/dist
USER app
EXPOSE 3001
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD curl -fsS http://127.0.0.1:3001/api/health || exit 1
CMD ["/app/phantomframe"]
