FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26 AS go
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The Go package embeds web/build, so the UI must be in place before compiling.
COPY --from=web /src/web/build ./web/build
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/knoxmgr ./cmd/knoxmgr \
 && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/knoxmock ./cmd/knoxmock \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot AS knoxmock
COPY --from=go /out/knoxmock /usr/local/bin/knoxmock
EXPOSE 9090
ENTRYPOINT ["/usr/local/bin/knoxmock"]

# Last stage, so a plain `docker build` yields the manager.
FROM gcr.io/distroless/static-debian12:nonroot AS knoxmgr
COPY --from=go /out/knoxmgr /usr/local/bin/knoxmgr
# A named volume copies this ownership, so the nonroot user can write the database and keys.
COPY --from=go --chown=nonroot:nonroot /out/data /var/lib/knoxmgr
VOLUME /var/lib/knoxmgr
WORKDIR /var/lib/knoxmgr
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/knoxmgr"]
