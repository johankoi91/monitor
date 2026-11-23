# This exact PostgreSQL 17.11 base is also used by the existing deployment.
ARG POSTGRES_IMAGE=docker.m.daocloud.io/library/postgres@sha256:ae69c452f483507a6b99fb654cf93aad7fe156ffd2c56247707eef4e36d3c12b
FROM ${POSTGRES_IMAGE}
COPY ca-bundle.crt /etc/ssl/certs/ca-certificates.crt
COPY center center-init dbmigrate notification-receiver /
ENV PGDATA=/postgres AVOPS_DATA_DIR=/state AVOPS_PRIVATE_DIR=/private
LABEL avops.role="center" avops.layout="three-images" avops.deployment="compose-v1" avops.probe.schema="2"
USER 0:0
EXPOSE 19443 18443
STOPSIGNAL SIGTERM
ENTRYPOINT ["/center-init"]
CMD []

HEALTHCHECK --interval=15s --timeout=4s --start-period=120s --retries=3 CMD ["/center-init", "health"]
