# Dependencies

Every direct runtime dependency has a concrete role:

| Dependency | Role |
| --- | --- |
| `charm.land/bubbletea/v2` | Application event loop, terminal input, resize, mouse, paste, and screen renderer |
| `charm.land/lipgloss/v2` | Terminal-native layout and styling |
| `charm.land/bubbles/v2` | Maintained text input and viewport components |
| `github.com/aymanbagabas/go-pty` | Native Unix PTYs and Windows ConPTY behind one process interface |
| `github.com/charmbracelet/ultraviolet` | Lossless keyboard and mouse event representation shared by Bubble Tea and the VT emulator |
| `github.com/charmbracelet/x/vt` | VT/ANSI terminal state, alternate buffer, cursor, screen rendering, and terminal replies |
| `modernc.org/sqlite` | Pure-Go SQLite driver, enabling reproducible cross-compilation without CGO |

The standard library supplies networking, JSON framing, hashing, Git command
execution, scheduling primitives, file watching by polling, and update HTTP.
No provider SDK or provider-specific adapter is included.
