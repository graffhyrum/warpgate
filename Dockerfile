# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/warpgate ./cmd/warpgate

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/warpgate /bin/warpgate
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/bin/warpgate"]
