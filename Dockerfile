# syntax=docker/dockerfile:1
# Imagem final: só o binário estático (~9 MB) sobre "scratch" — sem shell, sem pacotes.

FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/brunos . \
 && mkdir -p /out/data

FROM scratch
LABEL org.opencontainers.image.title="BrunOS" \
      org.opencontainers.image.description="Painel leve para servidor caseiro com Runtipi, no estilo do umbrelOS"
COPY --from=build /out/brunos /brunos
# certificados das autoridades (~220 KB): sem eles nenhuma conexão HTTPS para fora funciona
# (Clima/Open-Meteo, integrações em https://)
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=1000:1000 /out/data /data
ENV BRUNOS_DATA=/data BRUNOS_ADDR=:8080
EXPOSE 8080
USER 1000:1000
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/brunos", "healthcheck"]
ENTRYPOINT ["/brunos"]
