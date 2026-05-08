FROM golang:1.23-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/app ./

FROM alpine:3.20
WORKDIR /app
RUN adduser -D -g '' appuser
COPY --from=build /out/app /app/app
COPY data /app/data
EXPOSE 8080
USER appuser
ENTRYPOINT ["/app/app"]
