"""Atlas from Python (spec §5.1).

A Python face over atlas-engine, the Go client run as a small program. The
engine does every protocol job: sign-in, discovery, validation, fingerprints,
calls, resolution, handoffs. The face converts, names and presents; it never
decides.
"""

from .client import FIAtlas, Table
from .errors import AtlasError, EngineError

__all__ = ["FIAtlas", "Table", "AtlasError", "EngineError"]
