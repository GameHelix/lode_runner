FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
# COMMIT is the source commit, reported by the health check; deploy.sh sets it.
ARG COMMIT=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY trader/api ./trader/api
COPY app ./app
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.commit=$COMMIT" -o /out/app ./app/cmd/app

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
CMD ["serve"]
