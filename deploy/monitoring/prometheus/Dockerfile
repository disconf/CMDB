FROM prom/prometheus:v3.13.1

COPY --chown=nobody:nobody prometheus.yml /etc/prometheus/prometheus.yml
COPY --chown=nobody:nobody rules /etc/prometheus/rules

USER nobody
EXPOSE 9090
