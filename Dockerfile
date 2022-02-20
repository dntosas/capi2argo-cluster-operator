ARG DISTROLESS_IMAGE "gcr.io/distroless/static:nonroot"

# Switch to distroless as minimal base image to package the capi2argo-cluster-operator binary
FROM "${DISTROLESS_IMAGE}"
WORKDIR /
COPY bin/capi2argo-cluster-operator .
USER 65532:65532
ENTRYPOINT ["/capi2argo-cluster-operator"]
