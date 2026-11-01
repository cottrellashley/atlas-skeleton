"""Atlas from Python (spec §5.1; ATLAS-CORE §6).

A Python face over atlas-engine, the Go client run as a small program. The
engine does every protocol job: sign-in, discovery, validation, fingerprints,
calls, retries, deadlines, resolution, handoffs. The face converts, names and
presents; it never decides.
"""

from . import errors
from .client import FIAtlas, Result, Table
from .errors import AtlasError, AtlasProblem, AtlasWarning, EngineError, TruncatedWarning
from .errors import CLASSES as _CLASSES

globals().update({cls.__name__: cls for cls in _CLASSES.values()})

__all__ = ["FIAtlas", "Result", "Table", "AtlasProblem", "AtlasError", "AtlasWarning", "TruncatedWarning", "EngineError", "errors",
           *(cls.__name__ for cls in _CLASSES.values())]
