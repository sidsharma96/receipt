"""Public lookup key for the existing read-only profiling fixture.

This is a cache key, not a credential. Remote probes must still authenticate and
fail if the server no longer retains it. Originally recorded in:
https://github.com/sidsharma96/receipt/blob/f87c9b5eb6ee5ba74ac1837181189732ceaebf7b/docs/measurements/application-profile-2026-10-20/server-metadata.json
"""

APPLICATION_KEY = '6ed67fa1038a474244a09954ae63b9093a0a1205dcfef9fc4888b399c7d9457c'
