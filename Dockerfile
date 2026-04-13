FROM gcr.io/distroless/static:nonroot
ARG BINARY=media-operator-servarr
WORKDIR /
COPY ${BINARY} /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
