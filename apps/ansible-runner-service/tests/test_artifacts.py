import os
os.environ.setdefault("RUNNER_API_TOKEN","0123456789abcdef-test-token")
from app.artifacts import ArtifactStore
from app.config import Settings
def test_artifact_store_disabled_without_endpoint():
    settings=Settings(s3_endpoint_url="")
    assert ArtifactStore(settings).enabled is False
def test_command_map_is_typed():
    settings=Settings(runner_command_map_json='{"check":"check.yml"}')
    assert settings.command_map=={"check":"check.yml"}
