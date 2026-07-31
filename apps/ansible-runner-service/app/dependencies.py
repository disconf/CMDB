import secrets
from typing import Annotated
from fastapi import Depends,HTTPException,status
from fastapi.security import HTTPAuthorizationCredentials,HTTPBearer
from app.config import Settings,get_settings
security=HTTPBearer(auto_error=False)
def require_token(credentials:Annotated[HTTPAuthorizationCredentials|None,Depends(security)],settings:Annotated[Settings,Depends(get_settings)])->None:
    if credentials is None or credentials.scheme.lower()!="bearer" or not secrets.compare_digest(credentials.credentials,settings.runner_api_token):raise HTTPException(status_code=status.HTTP_401_UNAUTHORIZED,detail="Invalid runner token",headers={"WWW-Authenticate":"Bearer"})
AuthDep=Annotated[None,Depends(require_token)]
SettingsDep=Annotated[Settings,Depends(get_settings)]
