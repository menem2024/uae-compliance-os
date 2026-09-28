# Tools image for the Helm `zitadel-bootstrap` Job: runs the compose
# bootstrap.sh unchanged (curl, jq, psql) and writes the OIDC client into the
# `zitadel-oidc` Secret (kubectl). The compose variant installs these at
# runtime as root; in Kubernetes every pod runs as non-root, so the tools are
# baked in here instead.
#   docker build -t zitadel-bootstrap:dev deploy/k3d/bootstrap-tools
FROM alpine:3.22.6
RUN apk add --no-cache bash curl jq postgresql17-client kubectl
USER 65532:65532
ENTRYPOINT ["/bin/bash"]
