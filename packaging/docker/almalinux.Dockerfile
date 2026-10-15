ARG GO_IMAGE=golang:1.26.0
ARG ALMA_IMAGE=almalinux:8.10
FROM ${GO_IMAGE} AS go-toolchain

FROM ${ALMA_IMAGE}

RUN dnf -y install gcc make rpm-build && dnf clean all

COPY --from=go-toolchain /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /src
