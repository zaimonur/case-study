FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.22

WORKDIR /app
COPY --from=build --chown=65532:65532 /out/api /app/api

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/api"]
