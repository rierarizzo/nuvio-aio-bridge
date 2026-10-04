# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bridge ./cmd/bridge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bridge /bridge
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/bridge"]
