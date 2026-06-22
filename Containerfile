# Build stage
FROM registry.access.redhat.com/ubi9/go-toolset:1.25.5 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

USER root
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -o control-plane ./cmd/control-plane

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

WORKDIR /app

COPY --from=builder /app/control-plane .

# OpenShift runs arbitrary UIDs in group 0; g+rwX keeps /app usable.
RUN chown -R 1001:0 /app && chmod -R g+rwX /app

USER 1001

EXPOSE 8080

ENTRYPOINT ["./control-plane"]
