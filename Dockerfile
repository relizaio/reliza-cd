FROM --platform=$BUILDPLATFORM golang:1.19.4-alpine3.17@sha256:a9b24b67dc83b3383d22a14941c2b2b2ca6a103d805cac6820fd1355943beaf1 as build-stage
WORKDIR /build
RUN apk add --update --no-cache cosign unzip 
ENV CGO_ENABLED=0
COPY . .
ARG TARGETOS
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build
ADD https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.4/kubeseal-0.24.4-linux-${TARGETARCH}.tar.gz ./kubeseal-0.24.4-linux-${TARGETARCH}.tar.gz
ADD https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.4/kubeseal-0.24.4-linux-${TARGETARCH}.tar.gz.sig ./kubeseal-0.24.4-linux-${TARGETARCH}.tar.gz.sig
ADD https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.4/cosign.pub ./cosign.pub
ADD https://get.helm.sh/helm-v3.13.2-linux-${TARGETARCH}.tar.gz ./helm-v3.13.2-linux-${TARGETARCH}.tar.gz
ADD https://storage.googleapis.com/kubernetes-release/release/v1.28.4/bin/linux/${TARGETARCH}/kubectl ./kubectl-${TARGETARCH}
ADD https://d7ge14utcyki8.cloudfront.net/reliza-cli-download/2023.09.11/reliza-cli-2023.09.11-linux-${TARGETARCH}.zip ./reliza-cli-2023.09.11-linux-${TARGETARCH}.zip
RUN sha256sum -c tools.${TARGETARCH}.sha256
RUN cosign verify-blob --key cosign.pub --signature kubeseal-0.24.4-linux-${TARGETARCH}.tar.gz.sig kubeseal-0.24.4-linux-${TARGETARCH}.tar.gz
RUN tar -xzvf kubeseal-0.24.4-linux-${TARGETARCH}.tar.gz
RUN tar -xzvf helm-v3.13.2-linux-${TARGETARCH}.tar.gz
RUN unzip reliza-cli-2023.09.11-linux-${TARGETARCH}.zip
RUN mv kubectl-${TARGETARCH} kubectl

FROM alpine:3.17.0@sha256:8914eb54f968791faf6a8638949e480fef81e697984fba772b3976835194c6d4 as release-stage
ARG TARGETARCH
ARG CI_ENV=noci
ARG GIT_COMMIT=git_commit_undefined
ARG GIT_BRANCH=git_branch_undefined
ARG VERSION=not_versioned

RUN mkdir -p /app/workspace && mkdir /app/tools
RUN adduser -u 1000 -D apprunner && chown apprunner:apprunner -R /app
COPY --from=build-stage --chown=apprunner:apprunner /build/reliza-cd /app/reliza-cd
COPY --from=build-stage --chown=apprunner:apprunner /build/kubeseal /app/tools/kubeseal
COPY --from=build-stage --chown=apprunner:apprunner /build/kubectl /app/tools/kubectl
COPY --from=build-stage --chown=apprunner:apprunner /build/reliza-cli /app/tools/reliza-cli
COPY --from=build-stage --chown=apprunner:apprunner /build/linux-${TARGETARCH}/helm /app/tools/helm
COPY --chown=apprunner:apprunner entrypoint.sh /entrypoint.sh

RUN chmod 0700 /entrypoint.sh && chmod 0700 /app/tools/kubectl
USER apprunner
RUN echo "version=$VERSION" > /app/version && echo "commit=$GIT_COMMIT" >> /app/version && echo "branch=$GIT_BRANCH" >> /app/version

LABEL org.opencontainers.image.revision $GIT_COMMIT
LABEL git_branch $GIT_BRANCH
LABEL ci_environment $CI_ENV
LABEL org.opencontainers.image.version $VERSION

ENTRYPOINT ["/entrypoint.sh"]