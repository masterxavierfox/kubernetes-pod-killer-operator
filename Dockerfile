# This file is a template, and might need editing before it works on your project.
FROM golang:1.16-alpine AS builder

# We'll likely need to add SSL root certificates
RUN apk --no-cache add ca-certificates

WORKDIR /usr/src/app

COPY . .
#COPY go.mod .
#COPY go.sum .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -v -a -installsuffix cgo -o butcherctl .

FROM scratch

# Since we started from scratch, we'll copy the SSL root certificates from the builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

WORKDIR /usr/local/bin

COPY --from=builder /usr/src/app/butcherctl .
CMD ["./butcherctl"]
