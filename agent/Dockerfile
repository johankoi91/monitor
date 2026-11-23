FROM scratch
COPY ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
COPY agent agent-init cadvisor /
LABEL avops.role="agent" avops.layout="three-images" avops.deployment="compose-v1"
USER 0:0
STOPSIGNAL SIGTERM
ENTRYPOINT ["/agent-init"]

HEALTHCHECK --interval=15s --timeout=4s --start-period=120s --retries=3 CMD ["/agent-init", "health"]
