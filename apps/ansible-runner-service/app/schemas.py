from pydantic import BaseModel,Field
class RunRequest(BaseModel):
    jobId:str=Field(min_length=1,max_length=120)
    command:str=Field(default="",max_length=20000)
    content:str=Field(default="",max_length=200000)
    playbookId:str=Field(default="",max_length=120)
    playbookVersion:int=Field(default=0,ge=0)
    parameters:dict[str,str]=Field(default_factory=dict)
    checkMode:bool=False
    targets:list[str]=Field(min_length=1,max_length=100)
    operator:str=Field(min_length=1,max_length=120)
    attempt:int=Field(ge=1,le=100)
    timeoutSeconds:int=Field(ge=10,le=86400)
    credentialId:str=Field(default="",max_length=120)
class RunEvent(BaseModel):progress:int=Field(ge=0,le=100);level:str;message:str
class TargetResult(BaseModel):target:str;assetId:str="";assetName:str="";status:str;rc:int=0;changed:bool=False;stdout:str="";stderr:str="";error:str="";durationMs:int=0
class RunResponse(BaseModel):status:str;message:str;events:list[RunEvent]=Field(default_factory=list);targets:list[TargetResult]=Field(default_factory=list);artifactKeys:list[str]=Field(default_factory=list)
class HealthResponse(BaseModel):status:str;profiles:int