# siem-reconciler runtime image (ghcr.io/obmondo/siem-reconciler).
#
# The binary is built outside the image and copied in from
# <context>/<TARGETPLATFORM>/siem-reconciler, the layout goreleaser's
# dockers_v2 uses; `make docker-siem-reconciler` builds the same layout.
FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/siem-reconciler /usr/local/bin/siem-reconciler

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/siem-reconciler"]
