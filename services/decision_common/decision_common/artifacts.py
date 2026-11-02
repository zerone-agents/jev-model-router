"""Verify a pinned model snapshot before offline loading."""
from .provision import ArtifactError, verify as verify_snapshot

__all__ = ['ArtifactError', 'verify_snapshot']
