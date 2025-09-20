# syntax=docker/dockerfile:1

FROM golang:1.23 AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o app ./main.go

FROM gcr.io/distroless/base-debian12:nonroot
WORKDIR /app
COPY --from=builder /src/app /app/app
ENV PORT=8080
USER nonroot:nonroot
ENTRYPOINT ["/app/app"]
