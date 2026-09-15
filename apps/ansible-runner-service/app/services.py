import asyncio,json,shutil,tempfile,threading
from urllib.parse import quote
from pathlib import Path
import ansible_runner,httpx
from app.config import Settings
from app.artifacts import ArtifactStore
from app.schemas import RunEvent,RunRequest,RunResponse

class ExecutionError(Exception):pass
class UnknownProfileError(ExecutionError):pass
class AssetResolutionError(ExecutionError):pass
class RunnerService:
    def __init__(self,settings:Settings):self.settings=settings;self.artifacts=ArtifactStore(settings)
    async def resolve_inventory(self,targets:list[str])->dict:
        if len(targets)>self.settings.max_targets:raise AssetResolutionError("target limit exceeded")
        token=self.settings.cmdb_internal_token or self.settings.cmdb_token
        internal=bool(self.settings.cmdb_internal_token)
        headers={"Authorization":f"Bearer {token}"} if token else {}
        hosts={}
        async with httpx.AsyncClient(base_url=self.settings.cmdb_base_url,headers=headers,timeout=10) as client:
            for asset_id in targets:
                endpoint=f"/internal/runner/cmdb/assets/{quote(asset_id, safe='')}" if internal else f"/api/v1/cmdb/assets/{quote(asset_id, safe='')}"
                response=await client.get(endpoint)
                if response.status_code!=200:raise AssetResolutionError(f"asset not found: {asset_id}")
                asset=response.json();address=asset.get("ip")
                if not address:raise AssetResolutionError(f"asset has no IP: {asset_id}")
                try:port=int(asset.get("sshPort") or self.settings.ssh_port)
                except (TypeError,ValueError):port=self.settings.ssh_port
                if port<1 or port>65535:port=self.settings.ssh_port
                hosts[asset_id]={"ansible_host":address,"ansible_port":port}
        common_args=f"-o UserKnownHostsFile={self.settings.known_hosts_path} -o StrictHostKeyChecking=yes"
        values={"ansible_user":self.settings.ssh_user,"ansible_ssh_common_args":common_args}
        key_path=Path(self.settings.ssh_private_key_path)
        if key_path.is_file():values["ansible_ssh_private_key_file"]=str(key_path)
        elif self.settings.ssh_password:values["ansible_password"]=self.settings.ssh_password
        return {"all":{"hosts":hosts,"vars":values}}
    async def execute(self,payload:RunRequest)->RunResponse:
        playbook=self.settings.command_map.get(payload.command)
        if not playbook:raise UnknownProfileError("command is not mapped to an approved playbook")
        playbook_path=(Path(self.settings.playbook_dir)/playbook).resolve();root=Path(self.settings.playbook_dir).resolve()
        if root not in playbook_path.parents or not playbook_path.is_file():raise UnknownProfileError("approved playbook is unavailable")
        key_path=Path(self.settings.ssh_private_key_path);known_hosts=Path(self.settings.known_hosts_path)
        if not key_path.is_file() and not self.settings.ssh_password:raise ExecutionError("SSH credential is unavailable")
        if not known_hosts.is_file():raise ExecutionError("known_hosts is unavailable")
        inventory=await self.resolve_inventory(payload.targets);events:list[RunEvent]=[];cancel=threading.Event()
        Path(self.settings.work_dir).mkdir(parents=True,exist_ok=True)
        work=Path(tempfile.mkdtemp(prefix=f"{payload.jobId}-",dir=self.settings.work_dir))
        try:
            inventory_path=work/"inventory.json";inventory_path.write_text(json.dumps(inventory),encoding="utf-8")
            def event_handler(event:dict)->bool:
                stdout=str(event.get("stdout","")).strip()
                if stdout:events.append(RunEvent(progress=min(90,10+len(events)*5),level="info",message=stdout[-1000:]))
                return True
            envvars={"HOME":self.settings.work_dir,"ANSIBLE_LOCAL_TEMP":f"{self.settings.work_dir}/ansible-local","ANSIBLE_HOST_KEY_CHECKING":"True","ANSIBLE_SSH_ARGS":f"-o UserKnownHostsFile={known_hosts} -o StrictHostKeyChecking=yes"}
            if key_path.is_file():envvars["ANSIBLE_PRIVATE_KEY_FILE"]=str(key_path)
            if self.settings.vault_password_file:envvars["ANSIBLE_VAULT_PASSWORD_FILE"]=self.settings.vault_password_file
            def run():return ansible_runner.run(private_data_dir=str(work),playbook=str(playbook_path),inventory=str(inventory_path),envvars=envvars,event_handler=event_handler,cancel_callback=cancel.is_set,quiet=True,timeout=payload.timeoutSeconds)
            try:
                result=await asyncio.to_thread(run)
                inventory_path.unlink(missing_ok=True)
            except asyncio.CancelledError:cancel.set();raise
            status="success" if result.rc==0 and result.status=="successful" else "cancelled" if result.status in {"canceled","timeout"} else "failed"
            events.append(RunEvent(progress=100 if status=="success" else min(99,10+len(events)*5),level="success" if status=="success" else "error",message=f"ansible-runner {result.status}, rc={result.rc}"))
            if self.artifacts.enabled:
                try:key=await self.artifacts.upload(work,payload.jobId,payload.attempt);events.append(RunEvent(progress=100 if status=="success" else min(99,10+len(events)*5),level="info",message=f"execution artifact archived: {key}"))
                except Exception:events.append(RunEvent(progress=100 if status=="success" else min(99,10+len(events)*5),level="warning",message="execution artifact archive failed"))
            return RunResponse(status=status,message="playbook completed" if status=="success" else f"playbook {result.status}",events=events)
        finally:shutil.rmtree(work,ignore_errors=True)
