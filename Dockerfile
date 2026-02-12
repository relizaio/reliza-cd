FROM --platform=$BUILDPLATFORM golang:1.25.6-alpine3.23@sha256:98e6cffc31ccc44c7c15d83df1d69891efee8115a5bb7ede2bf30a38af3e3c92 AS build-stage
WORKDIR /build
RUN apk add --update --no-cache cosign unzip 
ENV CGO_ENABLED=0
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build
ADD https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.35.0/kubeseal-0.35.0-linux-${TARGETARCH}.tar.gz ./kubeseal-0.35.0-linux-${TARGETARCH}.tar.gz
ADD https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.35.0/kubeseal-0.35.0-linux-${TARGETARCH}.tar.gz.sig ./kubeseal-0.35.0-linux-${TARGETARCH}.tar.gz.sig
ADD https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.35.0/cosign.pub ./cosign.pub
ADD https://get.helm.sh/helm-v3.19.0-linux-${TARGETARCH}.tar.gz ./helm-v3.19.0-linux-${TARGETARCH}.tar.gz
ADD https://dl.k8s.io/v1.33.7/kubernetes-client-linux-${TARGETARCH}.tar.gz ./kubernetes-client-linux-${TARGETARCH}.tar.gz
ADD https://d7ge14utcyki8.cloudfront.net/reliza-cli-download/2024.07.10/reliza-cli-2024.07.10-linux-${TARGETARCH}.zip ./reliza-cli-2024.07.10-linux-${TARGETARCH}.zip
RUN sha256sum -c tools.${TARGETARCH}.sha256
RUN sha256sum -c tools.${TARGETARCH}.sha512
RUN cosign verify-blob --key cosign.pub --signature kubeseal-0.35.0-linux-${TARGETARCH}.tar.gz.sig kubeseal-0.35.0-linux-${TARGETARCH}.tar.gz
RUN tar -xzvf kubeseal-0.35.0-linux-${TARGETARCH}.tar.gz
RUN tar -xzvf helm-v3.19.0-linux-${TARGETARCH}.tar.gz
RUN unzip reliza-cli-2024.07.10-linux-${TARGETARCH}.zip
RUN tar -xzf kubernetes-client-linux-${TARGETARCH}.tar.gz && \
    mv kubernetes/client/bin/kubectl kubectl

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS release-stage
ARG TARGETARCH
ARG CI_ENV=noci
ARG GIT_COMMIT=git_commit_undefined
ARG GIT_BRANCH=git_branch_undefined
ARG VERSION=not_versioned
ENV ARGO_HELM_VERSION=5.51.6
RUN mkdir /app && adduser -u 1000 -D apprunner && chown apprunner:apprunner -R /app
USER apprunner
RUN mkdir -p /app/workspace && mkdir /app/tools
USER root
COPY --from=build-stage --chown=apprunner:apprunner /build/reliza-cd /app/reliza-cd
COPY --from=build-stage --chown=apprunner:apprunner /build/kubeseal /app/tools/kubeseal
COPY --from=build-stage --chown=apprunner:apprunner /build/kubectl /app/tools/kubectl
COPY --from=build-stage --chown=apprunner:apprunner /build/reliza-cli /app/tools/reliza-cli
COPY --from=build-stage --chown=apprunner:apprunner /build/linux-${TARGETARCH}/helm /app/tools/helm
COPY --chown=apprunner:apprunner entrypoint.sh /entrypoint.sh

RUN chmod 0700 /entrypoint.sh && chmod 0700 /app/tools/kubectl
USER apprunner
RUN echo "version=$VERSION" > /app/version && echo "commit=$GIT_COMMIT" >> /app/version && echo "branch=$GIT_BRANCH" >> /app/version

LABEL org.opencontainers.image.revision=$GIT_COMMIT
LABEL git_branch=$GIT_BRANCH
LABEL ci_environment=$CI_ENV
LABEL org.opencontainers.image.version=$VERSION

ENTRYPOINT ["/entrypoint.sh"]