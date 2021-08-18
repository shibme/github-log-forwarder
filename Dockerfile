FROM golang:alpine AS build-env
WORKDIR /build
COPY . /build
RUN go build -o app

FROM alpine
COPY --from=build-env /build/app /app
CMD ["./app"]