# Build
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kran ./app

# Runtime — kran must talk to the host Docker daemon via a mounted socket.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/kran /usr/local/bin/kran
# Must be able to read the mounted Docker socket (commonly root:root 600 on the host).
ENTRYPOINT ["/usr/local/bin/kran"]
