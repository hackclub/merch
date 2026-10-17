# Production image for Orchard. Config comes entirely from env vars (12-factor).
FROM node:24-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build
# The app can't start if index.html and the chunks name different SvelteKit versions (a blank site).
RUN g=$(grep -o '__sveltekit_[a-z0-9]*' build/index.html | head -1) && grep -rq "$g" build/_app/immutable \
    || { echo "build/index.html ($g) doesn't match the app chunks"; exit 1; }

FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=server /out/server /app/server
COPY --from=web /web/build /app/web/build
ENV PORT=8080 STATIC_DIR=/app/web/build
EXPOSE 8080
ENTRYPOINT ["/app/server"]
