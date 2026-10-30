FROM python:3.12.12-slim-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir uv==0.9.28
ENV UV_PYTHON_DOWNLOADS=never UV_LINK_MODE=copy
WORKDIR /app
COPY pyproject.toml uv.lock model-manifest.json ./
COPY src ./src
RUN --mount=type=cache,target=/root/.cache/uv uv sync --frozen --no-dev --no-editable

FROM python:3.12.12-slim-bookworm
RUN useradd --uid 10001 --create-home service && mkdir /models && chown service:service /models
WORKDIR /app
COPY --from=build /app/.venv /app/.venv
COPY NOTICE.md /app/NOTICE.md
COPY licenses /app/licenses
ENV PATH="/app/.venv/bin:$PATH" HF_HUB_OFFLINE=1 TRANSFORMERS_OFFLINE=1 HF_HUB_DISABLE_TELEMETRY=1 TOKENIZERS_PARALLELISM=false USE_TF=0 PYTHONDONTWRITEBYTECODE=1
USER 10001:10001
EXPOSE 8090
HEALTHCHECK --interval=10s --timeout=3s --start-period=120s CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8090/health/ready', timeout=2)"
CMD ["uvicorn", "jev_laya.server:application", "--factory", "--host", "0.0.0.0", "--port", "8090", "--no-access-log", "--log-level", "warning"]
