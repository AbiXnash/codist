# Codist with everything inside: no Go, Node or Java needed on the host.
FROM node:22-slim AS ui
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.23-bookworm AS server
WORKDIR /src
COPY server/ ./
COPY --from=ui /web/dist/ internal/webui/dist/
RUN CGO_ENABLED=1 go build -o /codist .

FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates openjdk-21-jre-headless maven && rm -rf /var/lib/apt/lists/*
# The repository is mounted from the host, so git would otherwise refuse it as owned by someone else.
RUN git config --system --add safe.directory '*'
COPY --from=server /codist /usr/local/bin/codist
ENV CODIST_REPO=/repo CODIST_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["codist", "--no-open"]
