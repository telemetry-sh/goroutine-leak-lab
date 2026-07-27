FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/goroutine-leak-lab ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/goroutine-leak-lab /goroutine-leak-lab
EXPOSE 8080
ENTRYPOINT ["/goroutine-leak-lab"]
