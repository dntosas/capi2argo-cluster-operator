# Switch to distroless as minimal base image to package the capi2argo-cluster-operator binary
FROM "gcr.io/distroless/static:nonroot"
WORKDIR /
COPY bin/capi2argo-cluster-operator .
USER 65532:65532
ENTRYPOINT ["/capi2argo-cluster-operator"]
