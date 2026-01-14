# FlowScope Go Services
# Build any service with: docker build --build-arg SERVICE=agent|collector|api -t egressor/<service> .

FROM golang:1.22-alpine AS builder

ARG SERVICE=api

WORKDIR /app

RUN apk add --no-cache git make

COPY go.mod go.sum ./
RUN go mod download

COPY src/ ./src/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /egressor-${SERVICE} ./src/cmd/${SERVICE}

# Runtime
FROM alpine:3.19

ARG SERVICE=api

RUN apk add --no-cache ca-certificates

COPY --from=builder /egressor-${SERVICE} /usr/local/bin/egressor-service

# Default to non-root (agent will override with --privileged)
USER nobody:nobody

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/egressor-service"]
