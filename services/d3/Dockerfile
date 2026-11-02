FROM python:3.12.12-slim-bookworm AS build
RUN pip install --no-cache-dir uv==0.9.28
ENV UV_PYTHON_DOWNLOADS=never UV_LINK_MODE=copy
WORKDIR /app
COPY services/decision_common/pyproject.toml services/decision_common/uv.lock ./
COPY services/decision_common/decision_common ./decision_common
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-editable

FROM python:3.12.12-slim-bookworm
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/* && useradd --uid 10001 --create-home service && mkdir /models && chown service:service /models
WORKDIR /app
COPY --from=build /app/.venv /app/.venv
COPY services/d3/model-manifest.json /app/model-manifest.json
ENV PATH="/app/.venv/bin:$PATH" DECISION_BACKEND=d3 DECISION_MANIFEST=/app/model-manifest.json DECISION_MODEL_DIR=/models/current HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 USE_TF=0 PYTHONDONTWRITEBYTECODE=1 XDG_CACHE_HOME=/tmp/cache HF_MODULES_CACHE=/tmp/hf-modules
USER 10001:10001
EXPOSE 8090
HEALTHCHECK --interval=10s --timeout=3s --start-period=120s CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8090/health/ready', timeout=2)"
CMD ["uvicorn", "decision_common.server:application", "--factory", "--host", "0.0.0.0", "--port", "8090", "--no-access-log", "--log-level", "warning"]
