ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6
FROM ${RUNTIME_IMAGE}
WORKDIR /
COPY manager /manager
HEALTHCHECK NONE
USER 65532:65532
ENTRYPOINT ["/manager"]
