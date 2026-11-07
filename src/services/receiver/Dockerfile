

# set up go
FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
# build binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./services/receiver

# run the binary
FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/main .
ENTRYPOINT ["./main"]