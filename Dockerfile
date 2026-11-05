# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e

FROM golang:1.26.10-alpine3.23@sha256:34414731924cf2263160f0021f5cf6e1807382d85248f7cb419d4187b96d5e43 AS builder
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# Copy pre-generated templ files and pre-built static assets from CI
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.version=${VERSION} -X main.revision=${REVISION}" -o /out/dejaview ./cmd/dejaview \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/dejaview-migrate ./cmd/dejaview-migrate

FROM alpine:3.23.4@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11
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
CMD ["./dejaview"]
