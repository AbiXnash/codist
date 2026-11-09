# Codist in a container. Mount the repository to review at /repo and point Codist at your Ollama:
#
#   docker run --rm -p 8080:8080 -v "$PWD:/repo" -v codist-data:/data \
#     --add-host=host.docker.internal:host-gateway ghcr.io/abxengg/codist
#
# Then open http://localhost:8080.

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w" -tags "grammar_subset grammar_subset_java" -o /out/codist .

# Fixes are built and tested with Maven, and behaviour runs need a JDK, so the runtime has both, plus git.
FROM maven:3.9-eclipse-temurin-21
RUN apt-get update \
 && apt-get install -y --no-install-recommends git \
 && rm -rf /var/lib/apt/lists/* \
 && git config --system --add safe.directory '*'
COPY --from=build /out/codist /usr/local/bin/codist

ENV CODIST_ADDR=0.0.0.0:8080 \
    CODIST_DATA_DIR=/data \
    CODIST_NO_BROWSER=1 \
    CODIST_MODEL_URL=http://host.docker.internal:11434
VOLUME ["/data"]
EXPOSE 8080
WORKDIR /repo
ENTRYPOINT ["codist"]
CMD ["/repo"]
