"""Pinned model selection shared by provisioning and runtime."""
import os
from pathlib import Path

D3_MODELS = {
    'edge': 'vllm-sr/d3-edge',
    'lite': 'vllm-sr/d3-lite',
    'nano': 'vllm-sr/d3-nano',
    'mini': 'vllm-sr/d3-mini',
    'flash': 'vllm-sr/d3-flash',
    'd3': 'vllm-sr/d3',
}

def d3_model_id(variant):
    try:
        return D3_MODELS[variant]
    except KeyError:
        raise ValueError('Unsupported D3 variant') from None

def selected_manifest(kind, legacy_manifest=None):
    if kind == 'decider':
        path = os.environ.get('DECISION_MANIFEST') or legacy_manifest
        if not path:
            raise ValueError('Decision manifest is required')
        return Path(path), 'Mapika/decider-2b'
    if kind != 'd3':
        raise ValueError('Unsupported decision backend')
    variant = os.environ.get('D3_VARIANT', 'nano')
    model = d3_model_id(variant)
    directory = os.environ.get('D3_MANIFEST_DIR')
    if not directory:
        if variant != 'nano':
            raise ValueError('D3 variant manifests are unavailable')
        path = os.environ.get('DECISION_MANIFEST') or legacy_manifest
        if not path:
            raise ValueError('Decision manifest is required')
        return Path(path), model
    return Path(directory) / (variant + '.json'), model
