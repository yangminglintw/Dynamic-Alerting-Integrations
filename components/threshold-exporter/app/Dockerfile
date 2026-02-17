FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /threshold-exporter .

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=builder /threshold-exporter /usr/local/bin/threshold-exporter

EXPOSE 8080
ENTRYPOINT ["threshold-exporter"]
CMD ["-config", "/etc/threshold-exporter/config.yaml", "-listen", ":8080"]
