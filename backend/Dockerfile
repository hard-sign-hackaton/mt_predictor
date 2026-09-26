FROM golang:1.26-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/mt-backend . \
    && CGO_ENABLED=0 go build -o /out/ndtpfeeder ./cmd/ndtpfeeder

FROM golang:1.26-bookworm
WORKDIR /app
COPY --from=build /out/mt-backend /app/mt-backend
COPY --from=build /out/ndtpfeeder /app/ndtpfeeder
COPY data/generated /app/data/generated

EXPOSE 8080 9201
CMD ["/app/mt-backend", "-ndtp-addr", ":9201", "-http-addr", ":8080", "-catalog", "/app/data/generated/route_catalog.json"]
