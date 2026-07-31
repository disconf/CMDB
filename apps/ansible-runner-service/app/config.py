import json
from functools import lru_cache
from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict

class Settings(BaseSettings):
    model_config=SettingsConfigDict(env_file=".env",extra="ignore")
    runner_api_token:str=Field(min_length=16)
    cmdb_base_url:str="http://gateway-bff:8080"
    cmdb_token:str=""
    runner_command_map_json:str='{"health-check --full":"health-check.yml","install-node-exporter":"install-node-exporter.yml"}'
    playbook_dir:str="/opt/runner/playbooks"
    work_dir:str="/tmp/cmdb-runner"
    max_targets:int=100
    ssh_user:str="ansible"
    ssh_private_key_path:str="/run/secrets/ssh_private_key"
    known_hosts_path:str="/run/secrets/known_hosts"
    vault_password_file:str=""
    s3_endpoint_url:str=""
    s3_access_key_id:str=""
    s3_secret_access_key:str=""
    s3_bucket:str="cmdb-job-artifacts"
    s3_region:str="us-east-1"
    @property
    def command_map(self)->dict[str,str]:
        value=json.loads(self.runner_command_map_json)
        if not isinstance(value,dict) or not all(isinstance(k,str) and isinstance(v,str) for k,v in value.items()):raise ValueError("RUNNER_COMMAND_MAP_JSON must be an object")
        return value
@lru_cache
def get_settings()->Settings:return Settings()
