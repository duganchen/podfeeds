FROM golang:1.20 AS build-stage
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY templates/ /templates
RUN CGO_ENABLED=0 GOOS=linux go build -o /podfeeds
EXPOSE 8080

FROM gcr.io/distroless/base-debian11 AS build-release-stage
WORKDIR /
COPY --from=build-stage /podfeeds /podfeeds
COPY --from=build-stage /templates/ ./templates
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/podfeeds"]