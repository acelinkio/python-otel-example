from fastapi import FastAPI

# configure logging first so any later imports/logs use your formatter
from .log import configure_logging

configure_logging()

# install OTEL as early as possible (will allow subsequent logs to be forwarded)
from .otel import setup_otelproviders, instrument

setup_otelproviders()


import logging

log = logging.getLogger(__name__)

app = FastAPI(
    title="detailed",
    description="quick example application",
    root_path="/detailed",
)
instrument(app)


@app.get("/")
async def root():
    return {"message": "Hello World"}


# move top-level logging into startup so it runs after import/startup steps
@app.on_event("startup")
async def on_startup():
    log.info("info: YOU ROCK")
    log.warning("warn: I rock")
    log.error("error: dogs are amazing")
    log.error("error: cats are fluffy")
    log.error("error: lizards are bitey")
    log.error("error: sharks are bitey too")
