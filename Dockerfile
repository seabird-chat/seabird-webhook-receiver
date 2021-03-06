# Stage 1: Build the application
FROM golang:1.15 as builder

RUN mkdir /build

WORKDIR /code

ADD ./go.mod ./go.sum ./
RUN go mod download

ADD . ./
RUN go build -v -o /build/seabird-webhook-receiver ./cmd/seabird-webhook-receiver

# Stage 2: Copy files and configure what we need
FROM debian:buster-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

# Copy the built seabird into the container
COPY --from=builder /build /bin

EXPOSE 3000

ENTRYPOINT ["/bin/seabird-webhook-receiver"]
