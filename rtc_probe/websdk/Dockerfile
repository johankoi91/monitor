ARG PROBE_BASE_IMAGE=dockerproxy.net/library/golang:1.24-bookworm
FROM ${PROBE_BASE_IMAGE}
WORKDIR /opt/rtc-probe/websdk/custom-video
RUN if [ -f /etc/apt/sources.list.d/debian.sources ]; then sed -i 's@http://deb.debian.org@https://mirrors.aliyun.com@g' /etc/apt/sources.list.d/debian.sources; fi \
 && apt-get update && apt-get install -y --no-install-recommends xz-utils unzip ca-certificates
COPY build/node-linux.tar.xz /tmp/node-linux.tar.xz
RUN tar -xJf /tmp/node-linux.tar.xz -C /usr/local --strip-components=1 && rm /tmp/node-linux.tar.xz
COPY custom-video/package.json custom-video/package-lock.json ./
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
RUN npm ci --omit=dev --ignore-scripts --registry=https://registry.npmmirror.com
RUN apt-get install -y --no-install-recommends $(node -p "require('./node_modules/playwright-core/lib/server/registry/nativeDeps.js').deps['debian12-x64'].chromium.join(' ')") fonts-liberation \
 && rm -rf /var/lib/apt/lists/*
COPY build/chromium-headless-linux.zip /tmp/chromium.zip
RUN mkdir -p /ms-playwright/chromium_headless_shell-1194 \
 && unzip -q /tmp/chromium.zip -d /ms-playwright/chromium_headless_shell-1194 \
 && rm /tmp/chromium.zip \
 && useradd --uid 65532 --create-home rtc-web
ENV CHROME_PATH=/ms-playwright/chromium_headless_shell-1194/chrome-linux/headless_shell \
    AVOPS_PROBE_WEB_RUNTIME=/opt/rtc-probe/websdk/custom-video
COPY custom-video/server.mjs custom-video/worker.mjs custom-video/contract.mjs ./
COPY custom-video/browser/ ./browser/
COPY custom-video/sdk/ ./sdk/
COPY custom-video/fixtures/ ./fixtures/
COPY build/rtc-web-probe /rtc-web-probe
LABEL avops.probe.schema="2" avops.probe.service="rtc" avops.probe.client="web"
USER 65532:65532
ENTRYPOINT ["/rtc-web-probe"]
