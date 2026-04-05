FROM golang:1.26 AS builder

WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/arbiter ./cmd/arbiter/

FROM gcr.io/distroless/static-debian12

COPY --from=builder /bin/arbiter /bin/arbiter
USER nonroot:nonroot
ENTRYPOINT ["/bin/arbiter"]
