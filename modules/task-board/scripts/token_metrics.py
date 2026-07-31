"""Model-profile token estimates for complete canonical Task Board Markdown files."""

import math


# These are deliberately estimates, not provider tokenizer results. UTF-8 byte
# ratios remain conservative for Portuguese technical Markdown and code paths.
TOKEN_PROFILES = (
    ("codex", "Codex / GPT", 3.4),
    ("claude", "Claude", 2.6),
    ("gemini", "Gemini", 4.0),
    ("agy", "AGY / Agent", 3.0),
)


def calculate_markdown_token_metrics(markdown_source):
    source = str(markdown_source or "")
    utf8_bytes = len(source.encode("utf-8"))
    return {
        "estimated": True,
        "characters": len(source),
        "utf8_bytes": utf8_bytes,
        "words": len(source.split()),
        "lines": len(source.splitlines()),
        "profiles": {
            identifier: {
                "label": label,
                "tokens": math.ceil(utf8_bytes / bytes_per_token) if utf8_bytes else 0,
            }
            for identifier, label, bytes_per_token in TOKEN_PROFILES
        },
    }
