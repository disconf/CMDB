FROM prom/alertmanager:v0.32.1

COPY --chown=nobody:nobody alertmanager.yml /etc/alertmanager/alertmanager.yml

USER nobody
EXPOSE 9093
