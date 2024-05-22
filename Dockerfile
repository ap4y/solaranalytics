FROM docker.io/golang:alpine AS builder

RUN apk --no-cache add ca-certificates
ENV GO111MODULE=on
WORKDIR /app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -tags netgo -ldflags '-extldflags "-static" -s -w' -o /solaranalytics main.go

FROM scratch

COPY --from=builder /solaranalytics /solaranalytics
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 8080

ENTRYPOINT ["/solaranalytics"]
