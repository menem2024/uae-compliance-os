# Tools image for the idempotent Zitadel bootstrap job (compose service zitadel-bootstrap).
#   docker build -t zitadel-bootstrap:dev deploy/compose/zitadel
FROM alpine:3.22.6
# /generated is 1777 so a fresh named volume mounted there is writable by the job's
# non-root uid (compose runs it as HOST_UID so it can also write the host copy).
RUN apk add --no-cache curl jq postgresql17-client \
 && install -d -m 1777 /generated
COPY --chmod=0755 bootstrap.sh /usr/local/bin/bootstrap.sh
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/bootstrap.sh"]
