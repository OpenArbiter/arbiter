FROM golang:1.26.2 AS builder

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/arbiter ./cmd/arbiter/

FROM gcr.io/distroless/static-debian12

COPY --from=builder /app/arbiter /app/arbiter
COPY --from=builder /src/migrations /migrations
USER nonroot:nonroot
ENTRYPOINT ["/app/arbiter"]
