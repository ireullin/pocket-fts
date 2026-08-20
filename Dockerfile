# syntax=docker/dockerfile:1
FROM gcr.io/distroless/base-debian12

WORKDIR /app

COPY bin/pocket_fts /app/pocket_fts

EXPOSE 5122

ENTRYPOINT ["/app/pocket_fts"]
