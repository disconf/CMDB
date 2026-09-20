from fastapi import Depends,FastAPI,HTTPException
import asyncio
from fastapi.responses import StreamingResponse
from urllib.parse import quote
from app.dependencies import AuthDep,SettingsDep
from app.schemas import CapabilitiesResponse,HealthResponse,RunRequest,RunResponse
from app.artifacts import ArtifactStore
from app.services import AssetResolutionError,ExecutionError,RunnerService,UnknownProfileError
def get_service(settings:SettingsDep)->RunnerService:return RunnerService(settings)
def create_app()->FastAPI:
    app=FastAPI(title="CMDB Ansible Runner Service",version="0.1.0",docs_url=None,redoc_url=None)
    @app.get("/health",response_model=HealthResponse)
    async def health(settings:SettingsDep)->HealthResponse:return HealthResponse(status="ok",profiles=len(settings.command_map),version=settings.service_version,maxTargets=settings.max_targets,artifactArchive=bool(settings.s3_endpoint_url and settings.s3_access_key_id and settings.s3_secret_access_key))
    @app.get("/api/v1/capabilities",response_model=CapabilitiesResponse)
    async def capabilities(settings:SettingsDep,_:AuthDep)->CapabilitiesResponse:
        artifact_archive=bool(settings.s3_endpoint_url and settings.s3_access_key_id and settings.s3_secret_access_key)
        features=["playbook-runtime","check-mode","host-results","target-resolution","timeout-control"]
        if artifact_archive:features.append("artifact-archive")
        return CapabilitiesResponse(status="ok",version=settings.service_version,profiles=sorted(settings.command_map.keys()),maxTargets=settings.max_targets,artifactArchive=artifact_archive,features=features)
    @app.get("/api/v1/artifacts/{key:path}")
    async def artifact(key:str,_:AuthDep,settings:SettingsDep):
        if not key.startswith("jobs/") or not key.endswith(".zip"):raise HTTPException(status_code=400,detail="Invalid artifact key")
        body=await asyncio.to_thread(ArtifactStore(settings).open,key)
        filename=key.rsplit("/",1)[-1]
        return StreamingResponse(body.iter_chunks(chunk_size=1024*1024),media_type="application/zip",headers={"Content-Disposition":f"attachment; filename*=UTF-8''{quote(filename)}"})
    @app.post("/api/v1/run",response_model=RunResponse,dependencies=[Depends(lambda:None)])
    async def run(payload:RunRequest,_:AuthDep,service:RunnerService=Depends(get_service))->RunResponse:
        try:return await service.execute(payload)
        except UnknownProfileError as exc:raise HTTPException(status_code=422,detail=str(exc)) from exc
        except AssetResolutionError as exc:raise HTTPException(status_code=424,detail=str(exc)) from exc
        except ExecutionError as exc:raise HTTPException(status_code=424,detail=str(exc)) from exc
    return app
app=create_app()
