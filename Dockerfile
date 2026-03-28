FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /vrouter-server ./cmd/vrouter-server/

FROM alpine:3.21
RUN apk --no-cache add ca-certificates
COPY --from=builder /vrouter-server /usr/local/bin/vrouter-server
ENTRYPOINT ["vrouter-server"]
