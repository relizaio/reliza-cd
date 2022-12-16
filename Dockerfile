FROM alpine
ADD https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.18.0/kubeseal-0.18.0-linux-amd64.tar.gz /tmp/
COPY kubeseal_0.18.0_sha256 /tmp/
RUN cd /tmp && sha256sum -c kubeseal_0.18.0_sha256
RUN cd /tmp && tar -xzvf kubeseal-0.18.0-linux-amd64.tar.gz

ADD https://get.helm.sh/helm-v3.10.3-linux-amd64.tar.gz /tmp/