# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

FROM golang:1.27.3-alpine3.23@sha256:8a5046497cda39860b832ffdbe8dd66018a0f05bb01352f533a36e7e2ced66a0 AS builder
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Copy pre-generated templ files and pre-built static assets from CI
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=${VERSION} -X main.revision=${REVISION}" -o /out/dejaview ./cmd/dejaview \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/dejaview-migrate ./cmd/dejaview-migrate

FROM alpine:3.24.3@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
ARG LOG_LEVEL=info
ARG VERSION=dev
ARG REVISION=unknown
ARG SOURCE_URL=https://github.com/bitofbytes-io/dejaview
WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/dejaview ./dejaview
# The deploy hook runs /app/dejaview-migrate as a one-off job before each rollout.
COPY --from=builder /out/dejaview-migrate ./dejaview-migrate
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
# Liveness only. /health does not ping the database, so a database blip
# does not flap the container, and a wedged process still fails this check.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -q -O - http://127.0.0.1:4600/health || exit 1
CMD ["./dejaview"]
