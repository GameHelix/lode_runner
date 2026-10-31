FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
# COMMIT is the source commit, reported by the health check; deploy.sh sets it.
ARG COMMIT=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY notifier ./notifier
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.commit=$COMMIT" -o /out/notifier ./notifier/cmd/notifier

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/notifier /notifier
ENTRYPOINT ["/notifier"]
CMD ["serve"]
