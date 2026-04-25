# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Copy pre-generated templ files and pre-built static assets from CI
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=${VERSION} -X main.revision=${REVISION}" -o /out/dejaview ./cmd/dejaview

FROM alpine:3.20
ARG LOG_LEVEL=info
ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE_URL=https://github.com/bitofbytes-io/dejaview
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/dejaview ./dejaview
COPY --from=builder /src/static ./static
COPY migrations ./migrations

RUN addgroup -S dejaview \
    && adduser -S -G dejaview dejaview \
    && chown -R dejaview:dejaview /app

LABEL org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.title="dejaview" \
      org.opencontainers.image.description="DejaView web application"

ENV PORT=4600
ENV LOG_LEVEL=${LOG_LEVEL}
USER dejaview

EXPOSE 4600
CMD ["./dejaview"]


