import asyncio
import os

os.environ.setdefault("RUNNER_API_TOKEN", "0123456789abcdef-test-token")

from app.config import Settings
from app.services import RunnerService


class FakeResponse:
    def __init__(self, status_code=200, payload=None):
        self.status_code = status_code
        self.payload = payload or {}

    def json(self):
        return self.payload


class FakeClient:
    endpoint = ""
    params = None
    headers = {}
    payload = {"ip": "10.0.0.8", "sshPort": 2222}

    def __init__(self, **kwargs):
        self.auth_headers = kwargs.get("headers")
        FakeClient.headers = self.auth_headers

    async def __aenter__(self):
        return self

    async def __aexit__(self, exc_type, exc, traceback):
        return False

    async def get(self, endpoint, params=None):
        FakeClient.endpoint = endpoint
        FakeClient.params = params
        return FakeResponse(payload=FakeClient.payload)


def test_resolve_inventory_uses_internal_endpoint_and_asset_port(monkeypatch):
    monkeypatch.setattr("app.services.httpx.AsyncClient", FakeClient)
    FakeClient.payload = {"ip": "10.0.0.8", "sshPort": 2222}
    settings = Settings(
        runner_api_token="0123456789abcdef-test-token",
        cmdb_internal_token="internal-runner-token",
        ssh_user="root",
    )
    inventory = asyncio.run(RunnerService(settings).resolve_inventory(["srv/../1"]))
    assert FakeClient.endpoint == "/internal/runner/cmdb/assets/srv%2F..%2F1"
    assert FakeClient.headers["Authorization"] == "Bearer internal-runner-token"
    assert inventory["all"]["hosts"]["srv/../1"] == {"ansible_host": "10.0.0.8", "ansible_port": 2222}
    assert inventory["all"]["vars"]["ansible_user"] == "root"


def test_resolve_inventory_uses_password_when_key_is_unavailable(monkeypatch):
    monkeypatch.setattr("app.services.httpx.AsyncClient", FakeClient)
    FakeClient.payload = {"ip": "10.0.0.9"}
    settings = Settings(
        runner_api_token="0123456789abcdef-test-token",
        cmdb_internal_token="internal-runner-token",
        ssh_user="root",
        ssh_password="secret",
        ssh_private_key_path="/path/that/does/not/exist",
    )
    inventory = asyncio.run(RunnerService(settings).resolve_inventory(["srv-2"]))
    assert inventory["all"]["hosts"]["srv-2"]["ansible_port"] == 22
    assert inventory["all"]["vars"]["ansible_password"] == "secret"

def test_resolve_inventory_uses_vault_credential_per_asset(monkeypatch):
    monkeypatch.setattr("app.services.httpx.AsyncClient", FakeClient)
    FakeClient.payload = {
        "ip": "10.0.0.10",
        "sshPort": 2202,
        "credentialId": "cred-1",
        "sshUser": "ops",
        "sshPassword": "secret",
        "authType": "password",
    }
    settings = Settings(
        runner_api_token="0123456789abcdef-test-token",
        cmdb_internal_token="internal-runner-token",
        ssh_user="root",
        ssh_private_key_path="/path/that/does/not/exist",
    )
    inventory = asyncio.run(RunnerService(settings).resolve_inventory(["srv-3"], "cred-1"))
    assert FakeClient.params == {"credentialId": "cred-1"}
    host = inventory["all"]["hosts"]["srv-3"]
    assert host["ansible_user"] == "ops"
    assert host["ansible_password"] == "secret"
    assert host["ansible_connection"] == "paramiko"
    assert "PreferredAuthentications=password" in host["ansible_ssh_common_args"]
    assert "PubkeyAuthentication=no" in host["ansible_ssh_common_args"]
