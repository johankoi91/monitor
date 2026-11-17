# Build from the repository root after fetching the pinned SDK and demo files.
FROM golang:1.24-bookworm AS build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends pkg-config libavformat-dev libavcodec-dev libavutil-dev libswresample-dev && rm -rf /var/lib/apt/lists/*
COPY rtc_probe/ ./rtc_probe/
COPY Agora-Golang-Server-SDK-2.6.6/ ./Agora-Golang-Server-SDK-2.6.6/
WORKDIR /src/rtc_probe
RUN CGO_ENABLED=1 go build -tags agora -trimpath -ldflags "-s -w" -o /rtc-probe ./cmd/rtc-probe

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libstdc++6 libavformat59 libavcodec59 libavutil57 libswresample4 && rm -rf /var/lib/apt/lists/*
COPY --from=build /rtc-probe /rtc-probe
COPY Agora-Golang-Server-SDK-2.6.6/agora_sdk/ /opt/agora_sdk/
COPY rtc_probe/build/demo-media/ /opt/rtc-probe/test_data/
ENV LD_LIBRARY_PATH=/opt/agora_sdk
ENV RTC_PROBE_DATA_DIR=/data
USER 65532:65532
ENTRYPOINT ["/rtc-probe"]
