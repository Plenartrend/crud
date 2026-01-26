FROM golang:1.25.5-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git

RUN go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go generate ./src/openAPI/generate.go

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /app/server ./src

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/server .

RUN chown -R appuser:appuser /app

USER appuser

CMD ["./server"]

