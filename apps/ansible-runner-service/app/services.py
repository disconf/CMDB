import asyncio,json,shutil,tempfile,threading
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
        headers={"Authorization":f"Bearer {self.settings.cmdb_token}"} if self.settings.cmdb_token else {}
        hosts={}
        async with httpx.AsyncClient(base_url=self.settings.cmdb_base_url,headers=headers,timeout=10) as client:
            for asset_id in targets:
                response=await client.get(f"/api/v1/cmdb/assets/{asset_id}")
                if response.status_code!=200:raise AssetResolutionError(f"asset not found: {asset_id}")
                asset=response.json();address=asset.get("ip")
                if not address:raise AssetResolutionError(f"asset has no IP: {asset_id}")
                hosts[asset_id]={"ansible_host":address}
        return {"all":{"hosts":hosts,"vars":{"ansible_user":self.settings.ssh_user}}}
    async def execute(self,payload:RunRequest)->RunResponse:
        playbook=self.settings.command_map.get(payload.command)
        if not playbook:raise UnknownProfileError("command is not mapped to an approved playbook")
        playbook_path=(Path(self.settings.playbook_dir)/playbook).resolve();root=Path(self.settings.playbook_dir).resolve()
        if root not in playbook_path.parents or not playbook_path.is_file():raise UnknownProfileError("approved playbook is unavailable")
        key_path=Path(self.settings.ssh_private_key_path);known_hosts=Path(self.settings.known_hosts_path)
        if not key_path.is_file():raise ExecutionError("SSH private key is unavailable")
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
            envvars={"ANSIBLE_PRIVATE_KEY_FILE":str(key_path),"ANSIBLE_HOST_KEY_CHECKING":"True","ANSIBLE_SSH_ARGS":f"-o UserKnownHostsFile={known_hosts} -o StrictHostKeyChecking=yes"}
            if self.settings.vault_password_file:envvars["ANSIBLE_VAULT_PASSWORD_FILE"]=self.settings.vault_password_file
            def run():return ansible_runner.run(private_data_dir=str(work),playbook=str(playbook_path),inventory=str(inventory_path),envvars=envvars,event_handler=event_handler,cancel_callback=cancel.is_set,quiet=True,timeout=payload.timeoutSeconds)
            try:result=await asyncio.to_thread(run)
            except asyncio.CancelledError:cancel.set();raise
            status="success" if result.rc==0 and result.status=="successful" else "cancelled" if result.status in {"canceled","timeout"} else "failed"
            events.append(RunEvent(progress=100 if status=="success" else min(99,10+len(events)*5),level="success" if status=="success" else "error",message=f"ansible-runner {result.status}, rc={result.rc}"))
            if self.artifacts.enabled:
                try:key=await self.artifacts.upload(work,payload.jobId,payload.attempt);events.append(RunEvent(progress=100 if status=="success" else min(99,10+len(events)*5),level="info",message=f"execution artifact archived: {key}"))
                except Exception:events.append(RunEvent(progress=100 if status=="success" else min(99,10+len(events)*5),level="warning",message="execution artifact archive failed"))
            return RunResponse(status=status,message="playbook completed" if status=="success" else f"playbook {result.status}",events=events)
        finally:shutil.rmtree(work,ignore_errors=True)
