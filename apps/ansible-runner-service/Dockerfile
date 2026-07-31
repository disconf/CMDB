FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
RUN useradd --create-home --uid 10001 runner
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY app ./app
COPY playbooks /opt/runner/playbooks
RUN mkdir -p /tmp/cmdb-runner && chown -R runner:runner /tmp/cmdb-runner /opt/runner
USER runner
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 CMD ["python","-c","import urllib.request;urllib.request.urlopen('http://127.0.0.1:8090/health',timeout=3)"]
CMD ["uvicorn","app.main:app","--host","0.0.0.0","--port","8090","--workers","2"]
