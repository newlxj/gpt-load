FROM node:20-alpine AS builder

ARG VERSION=1.0.0
WORKDIR /build
COPY ./web .
RUN npm install
RUN VITE_VERSION=${VERSION} npm run build


FROM golang:alpine AS builder2

ARG VERSION=1.0.0
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux

WORKDIR /build

ADD go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=builder /build/dist ./web/dist
RUN go build -ldflags "-s -w -X aimanager/internal/version.Version=${VERSION}" -o aimanager


FROM alpine

WORKDIR /app
RUN apk upgrade --no-cache \
    && apk add --no-cache ca-certificates tzdata \
    && update-ca-certificates

COPY --from=builder2 /build/aimanager .
EXPOSE 3001
ENTRYPOINT ["/app/aimanager"]
