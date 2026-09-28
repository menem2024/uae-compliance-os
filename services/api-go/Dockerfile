FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY services/api-go/go.mod services/api-go/go.sum ./
RUN go mod download
COPY services/api-go .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
USER 65532:65532
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=5s --start-period=90s --retries=3 CMD ["/api", "healthcheck"]
ENTRYPOINT ["/api"]
