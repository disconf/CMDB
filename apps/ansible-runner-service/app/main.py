from fastapi import Depends,FastAPI,HTTPException
from app.dependencies import AuthDep,SettingsDep
from app.schemas import HealthResponse,RunRequest,RunResponse
from app.services import AssetResolutionError,ExecutionError,RunnerService,UnknownProfileError
def get_service(settings:SettingsDep)->RunnerService:return RunnerService(settings)
def create_app()->FastAPI:
    app=FastAPI(title="CMDB Ansible Runner Service",version="0.1.0",docs_url=None,redoc_url=None)
    @app.get("/health",response_model=HealthResponse)
    async def health(settings:SettingsDep)->HealthResponse:return HealthResponse(status="ok",profiles=len(settings.command_map))
    @app.post("/api/v1/run",response_model=RunResponse,dependencies=[Depends(lambda:None)])
    async def run(payload:RunRequest,_:AuthDep,service:RunnerService=Depends(get_service))->RunResponse:
        try:return await service.execute(payload)
        except UnknownProfileError as exc:raise HTTPException(status_code=422,detail=str(exc)) from exc
        except AssetResolutionError as exc:raise HTTPException(status_code=424,detail=str(exc)) from exc
        except ExecutionError as exc:raise HTTPException(status_code=424,detail=str(exc)) from exc
    return app
app=create_app()
