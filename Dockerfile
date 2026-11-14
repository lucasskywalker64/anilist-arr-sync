FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/lucasskywalker64/anilist-arr-sync"
ARG TARGETPLATFORM
COPY --chmod=755 ${TARGETPLATFORM}/anilist-arr-sync /usr/bin/anilist-arr-sync
COPY --chown=65532:65532 packaging/container-config/ /config/
ENV IN_DOCKER=1
VOLUME ["/config"]
EXPOSE 7171
ENTRYPOINT ["/usr/bin/anilist-arr-sync", "--no-browser", "--data-dir", "/config"]
