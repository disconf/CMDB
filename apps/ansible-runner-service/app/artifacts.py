import asyncio,shutil,tempfile
from pathlib import Path
from app.config import Settings
class ArtifactStore:
    def __init__(self,settings:Settings):self.settings=settings
    @property
    def enabled(self)->bool:return bool(self.settings.s3_endpoint_url)
    async def upload(self,work:Path,job_id:str,attempt:int)->str:
        if not self.enabled:return ""
        import boto3
        archive_base=Path(tempfile.gettempdir())/f"{job_id}-attempt-{attempt}"
        archive=Path(await asyncio.to_thread(shutil.make_archive,str(archive_base),"zip",str(work)))
        key=f"jobs/{job_id}/attempt-{attempt}.zip"
        try:
            client=boto3.client("s3",endpoint_url=self.settings.s3_endpoint_url,aws_access_key_id=self.settings.s3_access_key_id,aws_secret_access_key=self.settings.s3_secret_access_key,region_name=self.settings.s3_region)
            await asyncio.to_thread(client.upload_file,str(archive),self.settings.s3_bucket,key)
            return key
        finally:archive.unlink(missing_ok=True)
