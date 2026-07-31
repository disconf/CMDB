import os
os.environ.setdefault("RUNNER_API_TOKEN","0123456789abcdef-test-token")
from fastapi.testclient import TestClient
from app.main import app,get_service
from app.schemas import RunEvent,RunResponse
class FakeService:
    async def execute(self,payload):return RunResponse(status="success",message="done",events=[RunEvent(progress=100,level="success",message=payload.jobId)])
app.dependency_overrides[get_service]=lambda:FakeService()
client=TestClient(app)
payload={"jobId":"job-1","command":"health-check --full","targets":["srv-1"],"operator":"admin","attempt":1,"timeoutSeconds":30}
def test_requires_token():assert client.post("/api/v1/run",json=payload).status_code==401
def test_executes_approved_request():
    response=client.post("/api/v1/run",json=payload,headers={"Authorization":"Bearer 0123456789abcdef-test-token"})
    assert response.status_code==200
    assert response.json()["events"][0]["message"]=="job-1"
def test_health():assert client.get("/health").json()["status"]=="ok"
