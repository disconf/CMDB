import asyncio
import json
import shutil
import tempfile
import threading
from pathlib import Path
from urllib.parse import quote

import ansible_runner
import httpx

from app.artifacts import ArtifactStore
from app.config import Settings
from app.schemas import RunEvent, RunRequest, RunResponse, TargetResult


class ExecutionError(Exception):
    pass


class UnknownProfileError(ExecutionError):
    pass


class AssetResolutionError(ExecutionError):
    pass


class RunnerService:
    def __init__(self, settings: Settings):
        self.settings = settings
        self.artifacts = ArtifactStore(settings)

    def _host_key_policy(self) -> str:
        value = (self.settings.ssh_strict_host_key_checking or "accept-new").strip().lower()
        if value not in {"yes", "accept-new", "no"}:
            return "accept-new"
        return value

    async def resolve_inventory(
        self,
        targets: list[str],
        credential_id: str = "",
        known_hosts_path: str = "",
    ) -> dict:
        if len(targets) > self.settings.max_targets:
            raise AssetResolutionError("target limit exceeded")
        token = self.settings.cmdb_internal_token or self.settings.cmdb_token
        internal = bool(self.settings.cmdb_internal_token)
        headers = {"Authorization": f"Bearer {token}"} if token else {}
        requested_credential = (credential_id or "").strip()
        hosts: dict[str, dict] = {}
        async with httpx.AsyncClient(
            base_url=self.settings.cmdb_base_url,
            headers=headers,
            timeout=10,
        ) as client:
            for asset_id in targets:
                endpoint = (
                    f"/internal/runner/cmdb/assets/{quote(asset_id, safe='')}"
                    if internal
                    else f"/api/v1/cmdb/assets/{quote(asset_id, safe='')}"
                )
                params = {"credentialId": requested_credential} if requested_credential else None
                response = await client.get(endpoint, params=params)
                if response.status_code != 200:
                    raise AssetResolutionError(f"asset not found: {asset_id}")
                asset = response.json()
                address = asset.get("ip")
                if not address:
                    raise AssetResolutionError(f"asset has no IP: {asset_id}")
                try:
                    port = int(asset.get("sshPort") or self.settings.ssh_port)
                except (TypeError, ValueError):
                    port = self.settings.ssh_port
                if port < 1 or port > 65535:
                    port = self.settings.ssh_port

                host_vars = {"ansible_host": address, "ansible_port": port}
                ssh_user = str(asset.get("sshUser") or "").strip()
                ssh_password = str(asset.get("sshPassword") or "")
                if ssh_password:
                    host_vars["ansible_user"] = ssh_user or self.settings.ssh_user
                    host_vars["ansible_password"] = ssh_password
                    host_vars["ansible_connection"] = "paramiko"
                    host_vars["ansible_ssh_common_args"] = (
                        self._ssh_common_args(known_hosts_path)
                        + " -o PreferredAuthentications=password -o PubkeyAuthentication=no"
                    )
                elif ssh_user:
                    host_vars["ansible_user"] = ssh_user
                hosts[asset_id] = host_vars

        key_path = Path(self.settings.ssh_private_key_path)
        key_available = key_path.is_file()
        values = {
            "ansible_user": self.settings.ssh_user,
            "ansible_ssh_common_args": self._ssh_common_args(known_hosts_path),
        }
        if key_available:
            values["ansible_ssh_private_key_file"] = str(key_path)
        elif self.settings.ssh_password:
            values["ansible_connection"] = "paramiko"
            values["ansible_password"] = self.settings.ssh_password
        return {"all": {"hosts": hosts, "vars": values}}

    def _ssh_common_args(self, known_hosts_path: str = "") -> str:
        path = known_hosts_path or self.settings.known_hosts_path
        return (
            f"-o UserKnownHostsFile={path} "
            f"-o StrictHostKeyChecking={self._host_key_policy()}"
        )

    async def execute(self, payload: RunRequest) -> RunResponse:
        source_known_hosts = Path(self.settings.known_hosts_path)
        if not source_known_hosts.is_file():
            raise ExecutionError("known_hosts is unavailable")

        Path(self.settings.work_dir).mkdir(parents=True, exist_ok=True)
        work = Path(tempfile.mkdtemp(prefix=f"{payload.jobId}-", dir=self.settings.work_dir))
        events: list[RunEvent] = []
        target_results: dict[str, TargetResult] = {}
        artifact_keys: list[str] = []
        cancel = threading.Event()
        try:
            if payload.content.strip():
                playbook_path = work / "playbook.yml"
                playbook_path.write_text(payload.content, encoding="utf-8")
            else:
                playbook_name = self.settings.command_map.get(payload.command)
                if not playbook_name and payload.playbookId:
                    playbook_name = payload.playbookId + ".yml"
                if not playbook_name:
                    raise UnknownProfileError("command or playbook is not mapped to an approved playbook")
                playbook_path = (Path(self.settings.playbook_dir) / playbook_name).resolve()
                root = Path(self.settings.playbook_dir).resolve()
                if root not in playbook_path.parents or not playbook_path.is_file():
                    raise UnknownProfileError("approved playbook is unavailable")
            run_known_hosts = work / "known_hosts"
            shutil.copyfile(source_known_hosts, run_known_hosts)
            inventory = await self.resolve_inventory(
                payload.targets,
                payload.credentialId,
                str(run_known_hosts),
            )
            key_path = Path(self.settings.ssh_private_key_path)
            if not key_path.is_file() and not self.settings.ssh_password and not any(
                "ansible_password" in host for host in inventory["all"]["hosts"].values()
            ):
                raise ExecutionError("SSH credential is unavailable")
            inventory_path = work / "inventory.json"
            inventory_path.write_text(json.dumps(inventory), encoding="utf-8")

            def event_handler(event: dict) -> bool:
                stdout = str(event.get("stdout", "")).strip()
                if stdout:
                    events.append(
                        RunEvent(
                            progress=min(90, 10 + len(events) * 5),
                            level="info",
                            message=stdout[-1000:],
                        )
                    )
                event_name = str(event.get("event", ""))
                event_data = event.get("event_data") or {}
                host = str(event_data.get("host") or event.get("host") or "")
                response = event_data.get("res") or {}
                if host and event_name in {"runner_on_ok", "runner_on_failed", "runner_on_unreachable", "runner_on_skipped"}:
                    result_status = "success"
                    changed = bool(response.get("changed"))
                    if event_name == "runner_on_failed":
                        result_status = "failed"
                    elif event_name == "runner_on_unreachable":
                        result_status = "unreachable"
                    elif event_name == "runner_on_skipped":
                        result_status = "skipped"
                    target_results[host] = TargetResult(
                        target=host,
                        status=result_status,
                        rc=int(response.get("rc") or 0),
                        changed=changed,
                        stdout=str(response.get("stdout") or response.get("msg") or "")[-32000:],
                        stderr=str(response.get("stderr") or "")[-32000:],
                        error=str(response.get("msg") or "") if result_status not in {"success", "skipped"} else "",
                    )
                return True

            envvars = {
                "HOME": self.settings.work_dir,
                "ANSIBLE_LOCAL_TEMP": f"{self.settings.work_dir}/ansible-local",
                "ANSIBLE_HOST_KEY_CHECKING": "True",
                "ANSIBLE_SSH_ARGS": self._ssh_common_args(str(run_known_hosts)),
            }
            key_path = Path(self.settings.ssh_private_key_path)
            if key_path.is_file():
                envvars["ANSIBLE_PRIVATE_KEY_FILE"] = str(key_path)
            if self.settings.vault_password_file:
                envvars["ANSIBLE_VAULT_PASSWORD_FILE"] = self.settings.vault_password_file

            def run():
                return ansible_runner.run(
                    private_data_dir=str(work),
                    playbook=str(playbook_path),
                    inventory=str(inventory_path),
                    envvars=envvars,
                    extravars=payload.parameters,
                    event_handler=event_handler,
                    cancel_callback=cancel.is_set,
                    quiet=True,
                    timeout=payload.timeoutSeconds,
                )

            try:
                result = await asyncio.to_thread(run)
                inventory_path.unlink(missing_ok=True)
            except asyncio.CancelledError:
                cancel.set()
                raise
            status = (
                "success"
                if result.rc == 0 and result.status == "successful"
                else "cancelled"
                if result.status in {"canceled", "timeout"}
                else "failed"
            )
            events.append(
                RunEvent(
                    progress=100 if status == "success" else min(99, 10 + len(events) * 5),
                    level="success" if status == "success" else "error",
                    message=f"ansible-runner {result.status}, rc={result.rc}",
                )
            )
            if self.artifacts.enabled:
                try:
                    key = await self.artifacts.upload(work, payload.jobId, payload.attempt)
                    artifact_keys.append(key)
                    events.append(
                        RunEvent(
                            progress=100 if status == "success" else min(99, 10 + len(events) * 5),
                            level="info",
                            message=f"execution artifact archived: {key}",
                        )
                    )
                except Exception:
                    events.append(
                        RunEvent(
                            progress=100 if status == "success" else min(99, 10 + len(events) * 5),
                            level="warning",
                            message="execution artifact archive failed",
                        )
                    )
            return RunResponse(
                status=status,
                message="playbook completed" if status == "success" else f"playbook {result.status}",
                events=events,
                targets=list(target_results.values()),
                artifactKeys=artifact_keys,
            )
        finally:
            shutil.rmtree(work, ignore_errors=True)
