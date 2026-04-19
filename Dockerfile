FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /scraper ./main.go

FROM alpine:3.21

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /scraper /usr/local/bin/scraper

EXPOSE 8080

USER nobody

ENTRYPOINT ["/usr/local/bin/scraper"]
