ARG GO_IMAGE
ARG DISTROLESS_IMAGE

# Build capi2argo-cluster-operator binary
FROM "${GO_IMAGE}" as builder
WORKDIR /capi2argo-cluster-operator
ADD . .
RUN make build

# Switch to distroless as minimal base image to package the capi2argo-cluster-operator binary
FROM "${DISTROLESS_IMAGE}"
WORKDIR /
COPY --from=builder /capi2argo-cluster-operator/bin .
USER 65532:65532
ENTRYPOINT ["/capi2argo-cluster-operator"]
