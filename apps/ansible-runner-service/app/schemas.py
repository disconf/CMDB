from pydantic import BaseModel,Field
class RunRequest(BaseModel):
    jobId:str=Field(min_length=1,max_length=120)
    command:str=Field(min_length=1,max_length=500)
    targets:list[str]=Field(min_length=1,max_length=100)
    operator:str=Field(min_length=1,max_length=120)
    attempt:int=Field(ge=1,le=100)
    timeoutSeconds:int=Field(ge=10,le=86400)
class RunEvent(BaseModel):progress:int=Field(ge=0,le=100);level:str;message:str
class RunResponse(BaseModel):status:str;message:str;events:list[RunEvent]=Field(default_factory=list)
class HealthResponse(BaseModel):status:str;profiles:int
