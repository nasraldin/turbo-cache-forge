FROM golang:1.25 AS build
WORKDIR /src
COPY services/api/go.mod services/api/go.sum ./services/api/
RUN cd services/api && go mod download
COPY services/api ./services/api
RUN cd services/api && CGO_ENABLED=0 go build -o /out/server ./cmd/server

# Data dir owned by the non-root distroless uid, so a fresh named volume
# mounted over it at runtime inherits writable ownership.
FROM busybox:1.36 AS datadir
RUN mkdir -p /data && chown -R 65532:65532 /data

# Optional standalone goose image (e.g. to run Postgres migrations manually).
# The default + overlay both self-migrate on boot, so this is not required to
# run the stack. Ships the Postgres dialect set.
FROM golang:1.25 AS goose
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.27.2
COPY services/api/internal/db/migrations/postgres /migrations
ENTRYPOINT ["goose"]

FROM gcr.io/distroless/static-debian12:nonroot AS cache-api
COPY --from=build /out/server /server
COPY --from=datadir --chown=65532:65532 /data /data
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/server"]
