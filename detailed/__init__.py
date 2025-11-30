from fastapi import FastAPI
# configure logging first so any later imports/logs use your formatter
from .log import configure_logging
configure_logging()

# install OTEL as early as possible (will allow subsequent logs to be forwarded)
from .otel import setup_otelproviders
setup_otelproviders()

import logging
logger = logging.getLogger(__name__)

app = FastAPI(
    title="detailed",
    description="quick example application",
    root_path="/detailed",
)


@app.get("/")
async def root():
    return {"message": "Hello World"}