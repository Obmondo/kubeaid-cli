# siem-reconciler runtime image (ghcr.io/obmondo/siem-reconciler).
#
# The binary is built outside the image (goreleaser, or
# `make docker-siem-reconciler`) and copied in; the build context is the
# directory holding the linux binary named siem-reconciler.
FROM gcr.io/distroless/static-debian12:nonroot

COPY siem-reconciler /usr/local/bin/siem-reconciler

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/siem-reconciler"]
