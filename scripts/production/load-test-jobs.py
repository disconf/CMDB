"""Concurrent job-control-plane load test.

Usage:
    BASE_URL=http://gateway:8080 ACCESS_TOKEN=... JOB_COUNT=500 \
        python scripts/production/load-test-jobs.py

The test creates jobs from the built-in tpl-health template, starts all of
them, and reports HTTP status counts. Use a temporary gateway/database when
running against a shared environment.
"""

import collections
import concurrent.futures
import json
import os
import time
import urllib.error
import urllib.request

BASE = os.environ["BASE_URL"].rstrip("/")
TOKEN = os.environ["ACCESS_TOKEN"]
COUNT = int(os.environ.get("JOB_COUNT", "500"))
TARGET = os.environ.get("TARGET_ID", "loadtest-target-1")

HEADERS = {
    "Authorization": "Bearer " + TOKEN,
    "Content-Type": "application/json",
}


def post(path, payload=None, timeout=30):
    body = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    request = urllib.request.Request(BASE + path, data=body, headers=HEADERS, method="POST")
    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode("utf-8", "replace")[:300]
    except Exception as error:
        return 0, str(error)


def create_one(_):
    return post(
        "/api/v1/jobs/executions",
        {"templateId": "tpl-health", "targets": [TARGET], "timeoutSeconds": 300},
    )


def run_one(job_id):
    return post("/api/v1/jobs/executions/" + job_id + "/run")


started = time.time()
with concurrent.futures.ThreadPoolExecutor(max_workers=50) as pool:
    created = list(pool.map(create_one, range(COUNT)))

create_status = collections.Counter(item[0] for item in created)
job_ids = [item[1]["id"] for item in created if item[0] == 201 and isinstance(item[1], dict) and item[1].get("id")]
print("create_status", dict(create_status), "created", len(job_ids), "elapsed_s", round(time.time() - started, 2))

run_started = time.time()
with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, min(len(job_ids), 500))) as pool:
    run_results = list(pool.map(run_one, job_ids))
run_status = collections.Counter(item[0] for item in run_results)
print("run_status", dict(run_status), "submitted", len(run_results), "elapsed_s", round(time.time() - run_started, 2))
